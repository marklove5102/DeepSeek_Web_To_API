package metrics

import "github.com/go-chi/chi/v5"

func RegisterRoutes(r chi.Router, h *Handler) {
	r.Get("/metrics/overview", h.getOverviewMetrics)
}
