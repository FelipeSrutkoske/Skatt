package api

import (
	"net/http"

	"github.com/FelipeSrutkoske/Skatt/internal/domain"
)

type personInputRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Color    string `json:"color"`
	Archived bool   `json:"archived"`
}

func (s *Server) handleListPeople(w http.ResponseWriter, r *http.Request) {
	includeArchived := parseBoolQuery(r.URL.Query().Get("include_archived"))
	people, err := s.store.ListPeople(r.Context(), includeArchived)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, people)
}

func (s *Server) handleCreatePerson(w http.ResponseWriter, r *http.Request) {
	var req personInputRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	person, err := s.store.CreatePerson(r.Context(), req.Name, req.Email, req.Color)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, person)
}

func (s *Server) handleUpdatePerson(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var req personInputRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	person, err := s.store.UpdatePerson(r.Context(), id, req.Name, req.Email, req.Color, req.Archived)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, person)
}

func (s *Server) handleDeletePerson(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := s.store.DeletePerson(r.Context(), id); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetPersonLedger(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	ledger, err := s.store.GetPersonLedger(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ledger)
}

func (s *Server) handleCreatePayment(w http.ResponseWriter, r *http.Request) {
	var req domain.PaymentInput
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	payment, err := s.store.CreatePayment(r.Context(), req)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, payment)
}

func (s *Server) handleDeletePayment(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := s.store.DeletePayment(r.Context(), id); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
