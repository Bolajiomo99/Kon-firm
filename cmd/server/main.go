// Command server runs the Kon-firm backend: storefront API, POS endpoints,
// and the Monnify webhook listener, in a single binary.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	konfirm "github.com/Bolajiomo99/Kon-firm"
	"github.com/Bolajiomo99/Kon-firm/internal/api"
	"github.com/Bolajiomo99/Kon-firm/internal/auth"
	"github.com/Bolajiomo99/Kon-firm/internal/config"
	"github.com/Bolajiomo99/Kon-firm/internal/monnify"
	"github.com/Bolajiomo99/Kon-firm/internal/store"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log.Info("configuration loaded", "config", cfg.Redacted())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()

	// Migrations run on every boot; each one is idempotent, so a fresh clone
	// and a redeploy converge on the same schema with no extra step.
	if err := store.Migrate(ctx, st.Pool()); err != nil {
		return err
	}
	log.Info("migrations applied")

	// Seed the bootstrap admin. Someone has to be able to sign in before
	// anyone can be granted a role, so the first admin comes from config.
	if cfg.AdminPhone != "" && cfg.AdminPassword != "" {
		phone, err := auth.NormalizePhone(cfg.AdminPhone)
		if err != nil {
			return fmt.Errorf("KONFIRM_ADMIN_PHONE is not a valid Nigerian number: %w", err)
		}
		if err := auth.ValidatePassword(cfg.AdminPassword); err != nil {
			return fmt.Errorf("KONFIRM_ADMIN_PASSWORD rejected: %w", err)
		}
		hash, err := auth.HashPassword(cfg.AdminPassword)
		if err != nil {
			return err
		}
		if err := st.EnsureAdmin(ctx, phone, cfg.AdminName, hash); err != nil {
			return err
		}
		log.Info("admin account ready", "phone", auth.FormatPhoneForDisplay(phone))
	} else {
		// Loud, because an unreachable admin dashboard is a support call, and
		// a silently-absent one is worse than a missing feature.
		log.Warn("no admin configured — set KONFIRM_ADMIN_PHONE and KONFIRM_ADMIN_PASSWORD to enable the dashboard")
	}

	// Expired sessions are dead weight; clear them at boot rather than
	// carrying a table that only ever grows.
	if n, err := st.PurgeExpiredSessions(ctx); err != nil {
		log.Warn("could not purge expired sessions", "err", err)
	} else if n > 0 {
		log.Info("purged expired sessions", "count", n)
	}

	mc, err := monnify.NewClient(monnify.Config{
		APIKey:       cfg.MonnifyAPIKey,
		SecretKey:    cfg.MonnifySecretKey,
		ContractCode: cfg.MonnifyContractCode,
		BaseURL:      cfg.MonnifyBaseURL,
	})
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: api.NewServer(cfg, st, mc, log).Routes(konfirm.Frontend()),
		// Timeouts are not optional on a public listener: without them a slow
		// client can hold a connection open indefinitely.
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("server listening", "addr", srv.Addr, "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Info("shutting down")
	}

	// Give in-flight requests — a webhook mid-settlement, especially — a
	// chance to finish rather than cutting them off.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
