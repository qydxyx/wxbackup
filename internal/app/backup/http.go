package backup

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/wxbackup/wxbackup/internal/domain"
)

func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/accounts/{id}/backup", s.handleStart)
	mux.HandleFunc("GET /v1/accounts/{id}/backup/{job}", s.handleGet)
	mux.HandleFunc("DELETE /v1/accounts/{id}/backup/{job}", s.handleCancel)
}

func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	s.Register(mux)
	return mux
}

type startRequest struct {
	Mode domain.BackupMode `json:"mode"`
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
			writeErr(w, http.StatusBadRequest, ErrInvalidMode)
			return
		}
	}
	job, err := s.Start(r.Context(), r.PathValue("id"), req.Mode)
	if err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	writeJSON(w, http.StatusCreated, job)
}

func (s *Service) handleGet(w http.ResponseWriter, r *http.Request) {
	accountID := r.PathValue("id")
	jobID := r.PathValue("job")
	job, err := s.Get(r.Context(), accountID, jobID)
	if err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		s.streamSSE(w, r, job)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Service) handleCancel(w http.ResponseWriter, r *http.Request) {
	job, err := s.Cancel(r.Context(), r.PathValue("id"), r.PathValue("job"))
	if err != nil && !errors.Is(err, domain.ErrBackupCancelled) {
		writeErr(w, statusOf(err), err)
		return
	}
	if job.Status != domain.JobCancelled && errors.Is(err, domain.ErrBackupCancelled) {
		writeErr(w, statusOf(err), err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Service) streamSSE(w http.ResponseWriter, r *http.Request, job domain.BackupJob) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusOK, job)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	writeEvent := func(j domain.BackupJob) bool {
		payload, err := json.Marshal(j)
		if err != nil {
			return false
		}
		if _, err := w.Write([]byte("data: ")); err != nil {
			return false
		}
		if _, err := w.Write(payload); err != nil {
			return false
		}
		if _, err := w.Write([]byte("\n\n")); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	ch, unsub := s.Subscribe(job.ID)
	if ch == nil {
		_ = writeEvent(job)
		return
	}
	defer unsub()

	if job.Status.Terminal() {
		_ = writeEvent(job)
		return
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case next, ok := <-ch:
			if !ok {
				return
			}
			if !writeEvent(next) {
				return
			}
			if next.Status.Terminal() {
				return
			}
		}
	}
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
		case errors.Is(err, ErrNotFound):
			code = "not_found"
		case errors.Is(err, ErrConflict):
			code = "conflict"
		case errors.Is(err, ErrInvalidMode):
			code = "invalid_mode"
		case errors.Is(err, ErrNoSession):
			code = "no_session"
		}
	}
	writeJSON(w, status, errorBody{Error: errorPayload{Code: code, Message: msg}})
}

func statusOf(err error) int {
	switch {
	case errors.Is(err, ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, ErrInvalidMode):
		return http.StatusBadRequest
	case errors.Is(err, ErrNoSession):
		return http.StatusServiceUnavailable
	case errors.Is(err, ErrConflict),
		errors.Is(err, domain.ErrNotLoggedIn),
		errors.Is(err, domain.ErrBackupCancelled),
		errors.Is(err, domain.ErrSameWiFiRequired),
		errors.Is(err, domain.ErrPhoneNotForeground),
		errors.Is(err, domain.ErrDiscoveryPortInUse):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}
