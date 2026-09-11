package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/wxbackup/wxbackup/internal/domain"
	"github.com/wxbackup/wxbackup/internal/store/sqlite"
	"github.com/wxbackup/wxbackup/testdata/fixtures"
)

func TestFixtureSeed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, _ := seededStore(t)
	accts, err := store.ListAccounts(ctx)
	if err != nil || len(accts) != 1 {
		t.Fatalf("accounts %+v %v", accts, err)
	}
	if accts[0].ID != fixtures.AccountID || accts[0].WxID != fixtures.WxID {
		t.Fatalf("%+v", accts[0])
	}
	adb, err := store.OpenAccount(ctx, fixtures.WxID)
	if err != nil {
		t.Fatal(err)
	}
	convs, err := adb.ListConversations(ctx)
	if err != nil || len(convs) != 2 {
		t.Fatalf("conversations %+v %v", convs, err)
	}
	kinds := map[domain.ConversationKind]bool{}
	for _, c := range convs {
		kinds[c.Kind] = true
	}
	if !kinds[domain.ConversationFriend] || !kinds[domain.ConversationGroup] {
		t.Fatalf("kinds %v", kinds)
	}
	msgs, err := adb.ListMessages(ctx, fixtures.FriendTalkerID, nil, 20)
	if err != nil {
		t.Fatal(err)
	}
	types := map[int]bool{}
	for _, m := range msgs {
		types[m.MsgType] = true
	}
	for _, want := range []int{fixtures.MsgText, fixtures.MsgImage, fixtures.MsgVoice, fixtures.MsgVideo, fixtures.MsgSystem} {
		if !types[want] {
			t.Fatalf("missing message type %d in %v", want, types)
		}
	}
	miss, err := adb.GetMedia(ctx, fixtures.MissingMediaID)
	if err != nil || miss.Available {
		t.Fatalf("missing media %+v %v", miss, err)
	}
	got, err := adb.GetMedia(ctx, fixtures.AvailableMediaID)
	if err != nil || !got.Available {
		t.Fatalf("available media %+v %v", got, err)
	}
}

func TestListAccounts(t *testing.T) {
	t.Parallel()
	h := seededHandler(t)
	rec := do(t, h, http.MethodGet, "/v1/accounts")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.Bytes())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type %q", ct)
	}
	raw := rec.Body.String()
	if strings.Contains(raw, "not-serialized") || strings.Contains(raw, "access_pwd") {
		t.Fatalf("password hash leaked: %s", raw)
	}
	var body struct {
		Accounts []domain.Account `json:"accounts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Accounts) != 1 || body.Accounts[0].ID != fixtures.AccountID {
		t.Fatalf("%+v", body.Accounts)
	}
}

func TestListConversations(t *testing.T) {
	t.Parallel()
	h := seededHandler(t)
	rec := do(t, h, http.MethodGet, "/v1/conversations?account_id="+fixtures.AccountID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.Bytes())
	}
	var body struct {
		Conversations []domain.Conversation `json:"conversations"`
	}
	decode(t, rec, &body)
	if len(body.Conversations) != 2 {
		t.Fatalf("%+v", body.Conversations)
	}
	if body.Conversations[0].TalkerID != fixtures.FriendTalkerID {
		t.Fatalf("expected friend first, got %+v", body.Conversations)
	}
	if body.Conversations[0].Kind != domain.ConversationFriend || body.Conversations[1].Kind != domain.ConversationGroup {
		t.Fatalf("%+v", body.Conversations)
	}
}

func TestListConversationsRequiresAccount(t *testing.T) {
	t.Parallel()
	h := seededHandler(t)
	rec := do(t, h, http.MethodGet, "/v1/conversations")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rec.Code)
	}
	assertErrorCode(t, rec, "invalid_request")
}

func TestListConversationsUnknownAccount(t *testing.T) {
	t.Parallel()
	h := seededHandler(t)
	rec := do(t, h, http.MethodGet, "/v1/conversations?account_id=missing")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d", rec.Code)
	}
	assertErrorCode(t, rec, "not_found")
}

func TestListMessagesPages(t *testing.T) {
	t.Parallel()
	h := seededHandler(t)
	path := "/v1/conversations/" + fixtures.FriendTalkerID + "/messages?account_id=" + fixtures.AccountID + "&limit=2"
	rec := do(t, h, http.MethodGet, path)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.Bytes())
	}
	var page1 struct {
		Messages   []domain.Message `json:"messages"`
		NextBefore string           `json:"next_before"`
	}
	decode(t, rec, &page1)
	if len(page1.Messages) != 2 || page1.Messages[0].MsgID != "m6" || page1.Messages[1].MsgID != "m5" {
		t.Fatalf("page1 %+v", page1.Messages)
	}
	if page1.NextBefore == "" {
		t.Fatal("expected next_before")
	}

	path2 := "/v1/conversations/" + fixtures.FriendTalkerID + "/messages?account_id=" + fixtures.AccountID +
		"&limit=2&before=" + url.QueryEscape(page1.NextBefore)
	rec = do(t, h, http.MethodGet, path2)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.Bytes())
	}
	var page2 struct {
		Messages   []domain.Message `json:"messages"`
		NextBefore string           `json:"next_before"`
	}
	decode(t, rec, &page2)
	if len(page2.Messages) != 2 || page2.Messages[0].MsgID != "m4" || page2.Messages[1].MsgID != "m3" {
		t.Fatalf("page2 %+v", page2.Messages)
	}
	if page2.NextBefore == "" {
		t.Fatal("expected next_before")
	}

	rec = do(t, h, http.MethodGet, "/v1/conversations/"+fixtures.FriendTalkerID+"/messages?account_id="+fixtures.AccountID+"&limit=20")
	var all struct {
		Messages   []domain.Message `json:"messages"`
		NextBefore string           `json:"next_before"`
	}
	decode(t, rec, &all)
	if len(all.Messages) != 6 {
		t.Fatalf("want 6 got %d", len(all.Messages))
	}
	if all.NextBefore != "" {
		t.Fatalf("unexpected next_before %q", all.NextBefore)
	}
}

func TestListMessagesUnknownConversation(t *testing.T) {
	t.Parallel()
	h := seededHandler(t)
	rec := do(t, h, http.MethodGet, "/v1/conversations/wxid_nope/messages?account_id="+fixtures.AccountID)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d", rec.Code)
	}
	assertErrorCode(t, rec, "not_found")
}

func TestListMessagesBadCursor(t *testing.T) {
	t.Parallel()
	h := seededHandler(t)
	rec := do(t, h, http.MethodGet, "/v1/conversations/"+fixtures.FriendTalkerID+"/messages?account_id="+fixtures.AccountID+"&before=not-a-cursor")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rec.Code)
	}
	assertErrorCode(t, rec, "invalid_request")
}

func TestGetMediaAvailable(t *testing.T) {
	t.Parallel()
	h := seededHandler(t)
	rec := do(t, h, http.MethodGet, "/v1/media/"+fixtures.AvailableMediaID+"?account_id="+fixtures.AccountID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.Bytes())
	}
	if rec.Body.String() != string(fixtures.AvailableMediaBytes) {
		t.Fatalf("body %q", rec.Body.String())
	}
}

func TestGetMediaNeverOpened(t *testing.T) {
	t.Parallel()
	h := seededHandler(t)
	rec := do(t, h, http.MethodGet, "/v1/media/"+fixtures.MissingMediaID+"?account_id="+fixtures.AccountID)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.Bytes())
	}
	assertErrorCode(t, rec, string(domain.CodeMediaNeverOpened))
	var body errorResponse
	decode(t, rec, &body)
	if body.Error.Message != domain.ErrMediaNeverOpened.Message {
		t.Fatalf("message %q", body.Error.Message)
	}
}

func TestGetMediaUnknownID(t *testing.T) {
	t.Parallel()
	h := seededHandler(t)
	rec := do(t, h, http.MethodGet, "/v1/media/does-not-exist?account_id="+fixtures.AccountID)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d", rec.Code)
	}
	assertErrorCode(t, rec, string(domain.CodeMediaNeverOpened))
}

func TestEmptyStoreAccounts(t *testing.T) {
	t.Parallel()
	store, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	mux := http.NewServeMux()
	New(store).Register(mux)
	rec := do(t, mux, http.MethodGet, "/v1/accounts")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"accounts":[]}` {
		t.Fatalf("body %s", rec.Body.String())
	}
}

func seededStore(t *testing.T) (*sqlite.Store, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := fixtures.Seed(context.Background(), store, dir); err != nil {
		t.Fatal(err)
	}
	return store, dir
}

func seededHandler(t *testing.T) http.Handler {
	t.Helper()
	store, _ := seededStore(t)
	mux := http.NewServeMux()
	New(store).Register(mux)
	return mux
}

func do(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.NewDecoder(rec.Body).Decode(v); err != nil {
		t.Fatalf("json: %v body %s", err, rec.Body.Bytes())
	}
}

func assertErrorCode(t *testing.T, rec *httptest.ResponseRecorder, want string) {
	t.Helper()
	var body errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json: %v body %s", err, rec.Body.Bytes())
	}
	if body.Error.Code != want {
		t.Fatalf("code %q want %q (%s)", body.Error.Code, want, rec.Body.Bytes())
	}
}

func TestMethodNotAllowed(t *testing.T) {
	t.Parallel()
	h := seededHandler(t)
	rec := do(t, h, http.MethodPost, "/v1/accounts")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status %d", rec.Code)
	}
	_, _ = io.Copy(io.Discard, rec.Body)
}
