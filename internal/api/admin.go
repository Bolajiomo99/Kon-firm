package api

import (
	"net/http"
	"time"

	"github.com/Bolajiomo99/Kon-firm/internal/store"
)

// SalesSummary is the admin dashboard's headline figures.
type SalesSummary struct {
	TotalRevenueKobo int64  `json:"totalRevenueKobo"`
	TotalRevenue     string `json:"totalRevenue"`
	PaidOrders       int    `json:"paidOrders"`
	PendingOrders    int    `json:"pendingOrders"`
	FailedOrders     int    `json:"failedOrders"`
	OnlineOrders     int    `json:"onlineOrders"`
	POSOrders        int    `json:"posOrders"`
	LowStockCount    int    `json:"lowStockCount"`
}

type recentOrder struct {
	Reference string `json:"reference"`
	Customer  string `json:"customer"`
	// Kobo, not a formatted string: money is formatted in exactly one place
	// (the client's formatKobo), so the table and the stat tiles cannot
	// disagree about separators.
	TotalKobo int64     `json:"totalKobo"`
	Status    string    `json:"status"`
	Channel   string    `json:"channel"`
	CreatedAt time.Time `json:"createdAt"`
}

type adminOverview struct {
	Summary SalesSummary   `json:"summary"`
	Recent  []recentOrder  `json:"recent"`
	Refunds []store.Refund `json:"refunds"`
}

// handleAdminOverview reports sales figures.
//
// Revenue counts only orders that reached 'paid' — that is, orders settled by
// a signature-verified webhook. Pending orders are intent, not income, and
// counting them would overstate the merchant's books.
func (s *Server) handleAdminOverview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var sum SalesSummary

	err := s.store.Pool().QueryRow(ctx, `
		SELECT
			COALESCE(SUM(total_kobo) FILTER (WHERE status = 'paid'), 0),
			COUNT(*) FILTER (WHERE status = 'paid'),
			COUNT(*) FILTER (WHERE status = 'pending'),
			COUNT(*) FILTER (WHERE status = 'failed'),
			COUNT(*) FILTER (WHERE status = 'paid' AND channel = 'online'),
			COUNT(*) FILTER (WHERE status = 'paid' AND channel = 'pos')
		FROM orders`).
		Scan(&sum.TotalRevenueKobo, &sum.PaidOrders, &sum.PendingOrders,
			&sum.FailedOrders, &sum.OnlineOrders, &sum.POSOrders)
	if err != nil {
		s.log.Error("admin summary", "err", err)
		writeError(w, http.StatusInternalServerError, "could not load summary")
		return
	}
	sum.TotalRevenue = koboToNaira(sum.TotalRevenueKobo)

	if err := s.store.Pool().QueryRow(ctx,
		`SELECT COUNT(*) FROM products WHERE active AND stock <= 5`).Scan(&sum.LowStockCount); err != nil {
		s.log.Error("low stock count", "err", err)
		writeError(w, http.StatusInternalServerError, "could not load summary")
		return
	}

	rows, err := s.store.Pool().Query(ctx, `
		SELECT reference, customer_name, total_kobo, status::text, channel::text, created_at
		FROM orders ORDER BY created_at DESC LIMIT 20`)
	if err != nil {
		s.log.Error("recent orders", "err", err)
		writeError(w, http.StatusInternalServerError, "could not load orders")
		return
	}
	defer rows.Close()

	recent := []recentOrder{}
	for rows.Next() {
		var o recentOrder
		if err := rows.Scan(&o.Reference, &o.Customer, &o.TotalKobo, &o.Status, &o.Channel, &o.CreatedAt); err != nil {
			s.log.Error("scan order", "err", err)
			writeError(w, http.StatusInternalServerError, "could not load orders")
			return
		}
		recent = append(recent, o)
	}
	if err := rows.Err(); err != nil {
		s.log.Error("iterate orders", "err", err)
		writeError(w, http.StatusInternalServerError, "could not load orders")
		return
	}

	refunds, err := s.store.RecentRefunds(ctx, 20)
	if err != nil {
		s.log.Error("recent refunds", "err", err)
		writeError(w, http.StatusInternalServerError, "could not load refunds")
		return
	}

	writeJSON(w, http.StatusOK, adminOverview{Summary: sum, Recent: recent, Refunds: refunds})
}
