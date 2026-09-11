package httpapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/wxbackup/wxbackup/internal/adapter/media"
	"github.com/wxbackup/wxbackup/internal/app/stats"
	"github.com/wxbackup/wxbackup/internal/domain"
	"github.com/wxbackup/wxbackup/internal/store/fts"
	"github.com/wxbackup/wxbackup/internal/store/sqlite"
)

const (
	defaultMessageLimit = 50
	maxMessageLimit     = 1000
)

const (
	codeInvalidRequest = domain.Code("invalid_request")
	codeNotFound       = domain.Code("not_found")
)

var (
	errAccountIDRequired = &domain.Error{Code: codeInvalidRequest, Message: "account_id is required"}
	errAccountNotFound   = &domain.Error{Code: codeNotFound, Message: "account not found"}
	errConversationGone  = &domain.Error{Code: codeNotFound, Message: "conversation not found"}
	errBadCursor         = &domain.Error{Code: codeInvalidRequest, Message: "invalid before cursor"}
	errBadLimit          = &domain.Error{Code: codeInvalidRequest, Message: "invalid limit"}
	errBadMsgType        = &domain.Error{Code: codeInvalidRequest, Message: "invalid msg_type"}
	errBadTimeRange      = &domain.Error{Code: codeInvalidRequest, Message: "invalid from or to"}
	errBadTranscode      = &domain.Error{Code: codeInvalidRequest, Message: "transcode must be mp3 for voice"}
)

type Server struct {
	store *sqlite.Store
}

func New(store *sqlite.Store) *Server {
	return &Server{store: store}
}

func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/accounts", s.handleAccounts)
	mux.HandleFunc("GET /v1/accounts/{id}/settings", s.handleGetSettings)
	mux.HandleFunc("PUT /v1/accounts/{id}/settings", s.handlePutSettings)
	mux.HandleFunc("PUT /v1/accounts/{id}/password", s.handlePutPassword)
	mux.HandleFunc("POST /v1/accounts/{id}/password/verify", s.handleVerifyPassword)
	mux.HandleFunc("DELETE /v1/accounts/{id}", s.handleDeleteAccount)
	mux.HandleFunc("GET /v1/conversations", s.handleConversations)
	mux.HandleFunc("GET /v1/conversations/{id}/messages", s.handleMessages)
	mux.HandleFunc("GET /v1/search", s.handleSearch)
	mux.HandleFunc("GET /v1/media/{id}", s.handleMedia)
	mux.HandleFunc("GET /v1/stats", s.handleStats)
}

func (s *Server) handleAccounts(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListAccounts(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if list == nil {
		list = []domain.Account{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": list})
}

func (s *Server) handleConversations(w http.ResponseWriter, r *http.Request) {
	adb, _, err := s.accountDB(r)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	list, err := adb.ListConversations(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if list == nil {
		list = []domain.Conversation{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"conversations": list})
}

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	adb, _, err := s.accountDB(r)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	talkerID := r.PathValue("id")
	if talkerID == "" {
		writeError(w, http.StatusBadRequest, &domain.Error{Code: codeInvalidRequest, Message: "conversation id is required"})
		return
	}
	if _, err := adb.GetConversation(r.Context(), talkerID); err != nil {
		if errors.Is(err, sqlite.ErrNotFound) {
			writeError(w, http.StatusNotFound, errConversationGone)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeError(w, http.StatusBadRequest, errBadLimit)
		return
	}
	var before *sqlite.MessageCursor
	if raw := strings.TrimSpace(r.URL.Query().Get("before")); raw != "" {
		c, err := decodeBefore(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, errBadCursor)
			return
		}
		before = &c
	}
	// Peek one extra row so a full last page does not look like has-more.
	msgs, err := adb.ListMessages(r.Context(), talkerID, before, limit+1)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if msgs == nil {
		msgs = []domain.Message{}
	}
	body := map[string]any{"messages": msgs}
	if len(msgs) > limit {
		msgs = msgs[:limit]
		body["messages"] = msgs
		body["next_before"] = encodeBefore(sqlite.CursorFromMessage(msgs[len(msgs)-1]))
	}
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	adb, _, err := s.accountDB(r)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeError(w, http.StatusBadRequest, errBadLimit)
		return
	}
	q := fts.Query{
		Q:     r.URL.Query().Get("q"),
		Limit: limit,
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("msg_type")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, errBadMsgType)
			return
		}
		q.MsgType = &n
	}
	from, err := parseTimeParam(r.URL.Query().Get("from"))
	if err != nil {
		writeError(w, http.StatusBadRequest, errBadTimeRange)
		return
	}
	to, err := parseTimeParam(r.URL.Query().Get("to"))
	if err != nil {
		writeError(w, http.StatusBadRequest, errBadTimeRange)
		return
	}
	q.From, q.To = from, to
	msgs, err := fts.Search(r.Context(), adb.Conn(), q)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if msgs == nil {
		msgs = []domain.Message{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": msgs})
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	adb, acct, err := s.accountDB(r)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	snap, err := stats.Account(r.Context(), adb.Conn())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	snap.AccountID = acct.ID
	writeJSON(w, http.StatusOK, snap)
}

func (s *Server) handleMedia(w http.ResponseWriter, r *http.Request) {
	adb, _, err := s.accountDB(r)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	mediaID := r.PathValue("id")
	if mediaID == "" {
		writeError(w, http.StatusNotFound, domain.ErrMediaNeverOpened)
		return
	}
	m, err := adb.GetMedia(r.Context(), mediaID)
	if err != nil {
		if errors.Is(err, sqlite.ErrNotFound) {
			writeError(w, http.StatusNotFound, domain.ErrMediaNeverOpened)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if !m.Available || m.Path == "" {
		writeError(w, http.StatusNotFound, domain.ErrMediaNeverOpened)
		return
	}
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("transcode")))
	if format != "" {
		s.serveTranscoded(w, r, m, format)
		return
	}
	f, err := os.Open(m.Path)
	if err != nil {
		writeError(w, http.StatusNotFound, domain.ErrMediaNeverOpened)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		writeError(w, http.StatusNotFound, domain.ErrMediaNeverOpened)
		return
	}
	w.Header().Set("Content-Type", mediaContentType(m.Kind))
	http.ServeContent(w, r, mediaID, st.ModTime(), f)
}

func (s *Server) serveTranscoded(w http.ResponseWriter, r *http.Request, m domain.MediaObject, format string) {
	if format != media.FormatMP3 || m.Kind != domain.MediaKindVoice {
		writeError(w, http.StatusBadRequest, errBadTranscode)
		return
	}
	out, err := media.Transcode(r.Context(), m.Path, media.FormatMP3)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	w.Header().Set("Content-Type", "audio/mpeg")
	http.ServeContent(w, r, m.MediaID+".mp3", time.Now(), bytes.NewReader(out))
}

func (s *Server) accountDB(r *http.Request) (*sqlite.AccountDB, domain.Account, error) {
	id := strings.TrimSpace(r.URL.Query().Get("account_id"))
	if id == "" {
		return nil, domain.Account{}, errAccountIDRequired
	}
	ctx := r.Context()
	acct, err := s.store.GetAccount(ctx, id)
	if err != nil {
		if errors.Is(err, sqlite.ErrNotFound) {
			return nil, domain.Account{}, errAccountNotFound
		}
		return nil, domain.Account{}, err
	}
	adb, err := s.store.OpenAccount(ctx, acct.WxID)
	if err != nil {
		return nil, domain.Account{}, err
	}
	return adb, acct, nil
}

type errorResponse struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeAPIError(w http.ResponseWriter, err error) {
	if err == nil {
		writeError(w, http.StatusInternalServerError, errors.New("internal error"))
		return
	}
	if errors.Is(err, errAccountIDRequired) || errors.Is(err, errBadCursor) || errors.Is(err, errBadLimit) ||
		errors.Is(err, errBadMsgType) || errors.Is(err, errBadTimeRange) ||
		errors.Is(err, errBadTranscode) || errors.Is(err, media.ErrUnsupportedFormat) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if errors.Is(err, media.ErrFFmpegMissing) {
		writeError(w, http.StatusServiceUnavailable, err)
		return
	}
	if errors.Is(err, errAccountNotFound) || errors.Is(err, errConversationGone) || errors.Is(err, sqlite.ErrNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if de, ok := domain.AsError(err); ok {
		switch de.Code {
		case domain.CodeMediaNeverOpened:
			writeError(w, http.StatusNotFound, err)
			return
		case domain.CodePasswordRequired:
			writeError(w, http.StatusForbidden, err)
			return
		case domain.CodePasswordIncorrect:
			writeError(w, http.StatusUnauthorized, err)
			return
		}
	}
	writeError(w, http.StatusInternalServerError, err)
}

func writeError(w http.ResponseWriter, status int, err error) {
	code := "internal"
	msg := http.StatusText(status)
	if de, ok := domain.AsError(err); ok {
		code = string(de.Code)
		msg = de.Message
	} else if errors.Is(err, sqlite.ErrNotFound) {
		code = string(codeNotFound)
		msg = "not found"
	} else if err != nil && status >= http.StatusInternalServerError {
		msg = "internal error"
	} else if err != nil {
		msg = err.Error()
	}
	writeJSON(w, status, errorResponse{Error: errorBody{Code: code, Message: msg}})
}

func parseTimeParam(raw string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
		// 1e12 ms is 2001-09-09; chat timestamps after that are stored as millis.
		if n > 0 && n < 1_000_000_000_000 {
			t := time.Unix(n, 0).UTC()
			return &t, nil
		}
		t := time.UnixMilli(n).UTC()
		return &t, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		t, err = time.Parse(time.RFC3339Nano, raw)
	}
	if err != nil {
		return nil, errBadTimeRange
	}
	t = t.UTC()
	return &t, nil
}

func parseLimit(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultMessageLimit, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0, errBadLimit
	}
	if n == 0 {
		return defaultMessageLimit, nil
	}
	if n > maxMessageLimit {
		return maxMessageLimit, nil
	}
	return n, nil
}

func encodeBefore(c sqlite.MessageCursor) string {
	raw := strconv.FormatInt(c.CreateTime.UnixMilli(), 10) + "\n" +
		strconv.FormatInt(c.MsgSeq, 10) + "\n" + c.MsgID
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeBefore(s string) (sqlite.MessageCursor, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return sqlite.MessageCursor{}, err
	}
	parts := strings.SplitN(string(b), "\n", 3)
	if len(parts) != 3 || parts[2] == "" {
		return sqlite.MessageCursor{}, errBadCursor
	}
	ms, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return sqlite.MessageCursor{}, err
	}
	seq, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return sqlite.MessageCursor{}, err
	}
	return sqlite.MessageCursor{
		CreateTime: time.UnixMilli(ms).UTC(),
		MsgSeq:     seq,
		MsgID:      parts[2],
	}, nil
}

func mediaContentType(k domain.MediaKind) string {
	switch k {
	case domain.MediaKindImage:
		return "image/jpeg"
	case domain.MediaKindVideo:
		return "video/mp4"
	case domain.MediaKindVoice:
		return "audio/mpeg"
	default:
		return "application/octet-stream"
	}
}
