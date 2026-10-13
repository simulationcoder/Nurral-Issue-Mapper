// Package server serves the graph as JSON and the bundled web page.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/simulationcoder/nurral-issue-mapper/internal/graph"
)

// Handler serves GET /api/graph (?refresh=1 re-reads GitHub) and, when ui is
// not nil, the static page at /.
func Handler(svc *graph.Service, ui fs.FS) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/graph", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		refresh := r.URL.Query().Get("refresh")
		force := refresh == "1" || strings.EqualFold(refresh, "true")
		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()
		g, err := svc.Get(ctx, force)
		if err != nil {
			status := http.StatusBadGateway
			switch {
			case errors.Is(err, graph.ErrNoToken):
				status = http.StatusServiceUnavailable
			case errors.Is(err, context.DeadlineExceeded):
				status = http.StatusGatewayTimeout
			}
			writeJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, g)
	})
	if ui != nil {
		mux.Handle("/", http.FileServerFS(ui))
	}
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
