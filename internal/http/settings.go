package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/wxbackup/wxbackup/internal/app/accesspwd"
	"github.com/wxbackup/wxbackup/internal/domain"
	"github.com/wxbackup/wxbackup/internal/store/sqlite"
)

var (
	errPasswordEmpty = &domain.Error{Code: codeInvalidRequest, Message: "password is required"}
	errInvalidJSON   = &domain.Error{Code: codeInvalidRequest, Message: "invalid json"}
)

type accountSettings struct {
	BackupRoot  string `json:"backup_root"`
	HasPassword bool   `json:"has_password"`
}

type passwordBody struct {
	Password string `json:"password"`
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	acct, err := s.loadAccount(r)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settingsOf(acct))
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	acct, err := s.loadAccount(r)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	var body accountSettings
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	acct.BackupRoot = strings.TrimSpace(body.BackupRoot)
	if err := s.store.PutAccount(r.Context(), acct); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, settingsOf(acct))
}

func (s *Server) handlePutPassword(w http.ResponseWriter, r *http.Request) {
	acct, err := s.loadAccount(r)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	var body passwordBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if body.Password == "" {
		writeError(w, http.StatusBadRequest, errPasswordEmpty)
		return
	}
	hash, err := accesspwd.Hash(body.Password)
	if err != nil {
		writeError(w, http.StatusBadRequest, errPasswordEmpty)
		return
	}
	acct.AccessPwdHash = hash
	if err := s.store.PutAccount(r.Context(), acct); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "has_password": true})
}

func (s *Server) handleVerifyPassword(w http.ResponseWriter, r *http.Request) {
	acct, err := s.loadAccount(r)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	var body passwordBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := checkAccessPassword(acct, body.Password); err != nil {
		writeAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	acct, err := s.loadAccount(r)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	var body passwordBody
	if err := decodeJSONOptional(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := checkAccessPassword(acct, body.Password); err != nil {
		writeAPIError(w, err)
		return
	}
	if err := s.store.DeleteAccount(r.Context(), acct.WxID); err != nil {
		if errors.Is(err, sqlite.ErrNotFound) {
			writeAPIError(w, errAccountNotFound)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) loadAccount(r *http.Request) (domain.Account, error) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		return domain.Account{}, errAccountIDRequired
	}
	acct, err := s.store.GetAccount(r.Context(), id)
	if err != nil {
		if errors.Is(err, sqlite.ErrNotFound) {
			return domain.Account{}, errAccountNotFound
		}
		return domain.Account{}, err
	}
	return acct, nil
}

func settingsOf(acct domain.Account) accountSettings {
	return accountSettings{
		BackupRoot:  acct.BackupRoot,
		HasPassword: acct.HasPassword(),
	}
}

func checkAccessPassword(acct domain.Account, password string) error {
	if !acct.HasPassword() {
		return nil
	}
	if password == "" {
		return domain.ErrPasswordRequired
	}
	if !accesspwd.Verify(acct.AccessPwdHash, password) {
		return domain.ErrPasswordIncorrect
	}
	return nil
}

func decodeJSON(r *http.Request, v any) error {
	if r.Body == nil {
		return errInvalidJSON
	}
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		return errInvalidJSON
	}
	return nil
}

func decodeJSONOptional(r *http.Request, v any) error {
	if r.Body == nil {
		return nil
	}
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return errInvalidJSON
	}
	return nil
}
