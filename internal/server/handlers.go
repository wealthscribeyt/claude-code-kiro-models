package server

import (
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/d-kuro/kirocc/internal/httpx"
	"github.com/d-kuro/kirocc/internal/models"
)

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// hiddenModelPatterns reads KIROCC_HIDE_MODELS (comma-separated substrings,
// e.g. "fable,gpt-5.6-luna") listing advertised model IDs to hide from the
// /model picker — for SKUs the Kiro backend rejects for this account
// (Enterprise-only models) or the operator simply doesn't use.
func hiddenModelPatterns() []string {
	raw := os.Getenv("KIROCC_HIDE_MODELS")
	if raw == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(raw, ",") {
		if p = strings.ToLower(strings.TrimSpace(p)); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func modelHidden(id string, patterns []string) bool {
	lower := strings.ToLower(id)
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

func (s *Server) handleModels(w http.ResponseWriter, _ *http.Request) {
	modelList := models.ListModels()
	hidden := hiddenModelPatterns()
	data := make([]any, 0, len(modelList))
	now := time.Now().Unix()
	for _, m := range modelList {
		if modelHidden(m.ID, hidden) {
			continue
		}
		entry := map[string]any{
			"id":       m.ID,
			"object":   "model",
			"created":  now,
			"owned_by": "kiro",
		}
		if m.DisplayName != "" {
			entry["display_name"] = m.DisplayName
		}
		data = append(data, entry)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   data,
	})
}

// methodNotAllowed answers wrong-method hits on known paths with a 405 and
// an Allow header, so gateway clients see the correct status instead of the
// "/" catch-all's 404.
func methodNotAllowed(allow string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Allow", allow)
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

// handleNotFound is the catch-all for paths Claude Code probes behind a
// gateway (feature flags, org endpoints, ...). Anthropic-shaped JSON so the
// client degrades gracefully instead of choking on a plain-text 404.
func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusNotFound, map[string]any{
		"type": "error",
		"error": map[string]any{
			"type":    "not_found_error",
			"message": "Not proxied to Kiro: " + r.Method + " " + r.URL.Path,
		},
	})
}
