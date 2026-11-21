package api

import (
	"net/http"
	"strconv"
	"strings"
)

func (s *Server) handleGetDashboard(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	year := 0
	if rawYear := strings.TrimSpace(q.Get("year")); rawYear != "" {
		parsed, err := strconv.Atoi(rawYear)
		if err != nil || parsed < 0 {
			writeError(w, http.StatusBadRequest, "parametro year invalido")
			return
		}
		year = parsed
	}

	month := 0
	if rawMonth := strings.TrimSpace(q.Get("month")); rawMonth != "" {
		parsed, err := strconv.Atoi(rawMonth)
		if err != nil || parsed < 0 || parsed > 12 {
			writeError(w, http.StatusBadRequest, "parametro month invalido")
			return
		}
		month = parsed
	}

	bank := strings.TrimSpace(q.Get("bank"))

	summary, err := s.store.GetDashboard(r.Context(), year, month, bank)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, summary)
}
