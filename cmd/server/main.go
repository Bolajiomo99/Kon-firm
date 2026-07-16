// Command server runs the Kon-firm backend: storefront API, POS endpoints,
// and the Monnify webhook listener, in a single binary.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	konfirm "github.com/Bolajiomo99/Kon-firm"
	"github.com/Bolajiomo99/Kon-firm/internal/api"
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
