package verity

import (
	"errors"
	"net/http"
	"strconv"
)

func (s *HTTPServer) stageTable(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			s.problem(w, r, 422, "invalid_limit", "Limit must be between 1 and 100")
			return
		}
		limit = parsed
	}
	filter := r.URL.Query().Get("filter")
	if filter == "" {
		filter = "all"
	}
	page, err := s.store.Table(r.Context(), r.PathValue("stage_id"), r.URL.Query().Get("run_id"), r.URL.Query().Get("cursor"), filter, r.URL.Query().Get("q"), limit)
	if err != nil {
		status, code, detail := 500, "table_failed", "The stage table could not be opened."
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrConflict) {
			status, code, detail = 409, "snapshot_unavailable", "This snapshot is unavailable. Refresh the workspace or add data."
		}
		if errors.Is(err, ErrInvalidImport) {
			status, code, detail = 422, "invalid_table_query", err.Error()
		}
		s.problem(w, r, status, code, detail)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, page)
}
