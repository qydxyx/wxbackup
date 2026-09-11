package restore

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/wxbackup/wxbackup/internal/domain"
)

func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/accounts/{id}/restore", s.handleStart)
	mux.HandleFunc("GET /v1/accounts/{id}/restore/{job}", s.handleGet)
	mux.HandleFunc("POST /v1/accounts/{id}/export", s.handleExport)
}

func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	s.Register(mux)
	return mux
}

type startRequest struct {
	Selector   json.RawMessage `json:"selector"`
	SessionIDs []string        `json:"session_ids"`
}

type exportRequest struct {
	Dir        string          `json:"dir"`
	Selector   json.RawMessage `json:"selector"`
	SessionIDs []string        `json:"session_ids"`
}

type ExportResult struct {
	Dir   string   `json:"dir"`
	Files []string `json:"files"`
}

type errorBody struct {
	Error errorPayload `json:"error"`
}

type errorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (s *Service) handleStart(w http.ResponseWriter, r *http.Request) {
	var req startRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, ErrInvalidSelector)
			return
		}
	}
	sel, err := parseSelector(req.Selector, req.SessionIDs)
	if err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	job, err := s.Start(r.Context(), r.PathValue("id"), sel)
	if err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	writeJSON(w, http.StatusCreated, job)
}

func (s *Service) handleGet(w http.ResponseWriter, r *http.Request) {
	job, err := s.Get(r.Context(), r.PathValue("id"), r.PathValue("job"))
	if err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Service) handleExport(w http.ResponseWriter, r *http.Request) {
	var req exportRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, ErrExportDir)
			return
		}
	}
	sel, err := parseSelector(req.Selector, req.SessionIDs)
	if err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	got, err := s.Export(r.Context(), r.PathValue("id"), req.Dir, sel)
	if err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	writeJSON(w, http.StatusCreated, got)
}

func parseSelector(raw json.RawMessage, sessionIDs []string) (domain.RestoreSelector, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return domain.RestoreSelector{Kind: domain.RestoreAll, SessionIDs: sessionIDs}, nil
	}
	var kind string
	if err := json.Unmarshal(raw, &kind); err == nil {
		return domain.RestoreSelector{Kind: domain.RestoreSelectorKind(kind), SessionIDs: sessionIDs}, nil
	}
	var sel domain.RestoreSelector
	if err := json.Unmarshal(raw, &sel); err != nil {
		return domain.RestoreSelector{}, ErrInvalidSelector
	}
	if len(sel.SessionIDs) == 0 {
		sel.SessionIDs = sessionIDs
	}
	return sel, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, err error) {
	code := "error"
	msg := err.Error()
	if de, ok := domain.AsError(err); ok {
		code = string(de.Code)
		msg = de.Message
	} else {
		switch {
		case errors.Is(err, ErrNotFound), errors.Is(err, ErrUnknownSessionID):
			code = "not_found"
		case errors.Is(err, ErrConflict):
			code = "conflict"
		case errors.Is(err, ErrInvalidSelector):
			code = "invalid_selector"
		case errors.Is(err, ErrNoSession):
			code = "no_session"
		case errors.Is(err, ErrExportDir):
			code = "invalid_export_dir"
		}
	}
	writeJSON(w, status, errorBody{Error: errorPayload{Code: code, Message: msg}})
}

func statusOf(err error) int {
	switch {
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrUnknownSessionID):
		return http.StatusNotFound
	case errors.Is(err, ErrInvalidSelector), errors.Is(err, ErrExportDir):
		return http.StatusBadRequest
	case errors.Is(err, ErrNoSession):
		return http.StatusServiceUnavailable
	case errors.Is(err, ErrConflict), errors.Is(err, domain.ErrNotLoggedIn):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}
