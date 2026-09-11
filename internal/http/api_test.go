package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wxbackup/wxbackup/internal/adapter/media"
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
	base := "/v1/conversations/" + fixtures.FriendTalkerID + "/messages?account_id=" + fixtures.AccountID + "&limit=2"
	var ids []string
	before := ""
	for page := 0; page < 10; page++ {
		path := base
		if before != "" {
			path += "&before=" + url.QueryEscape(before)
		}
		rec := do(t, h, http.MethodGet, path)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d body %s", rec.Code, rec.Body.Bytes())
		}
		var body msgPage
		decode(t, rec, &body)
		if len(body.Messages) == 0 {
			t.Fatalf("empty page %d", page)
		}
		if len(body.Messages) > 2 {
			t.Fatalf("page %d over limit: %+v", page, idsOf(body.Messages))
		}
		for _, m := range body.Messages {
			ids = append(ids, m.MsgID)
		}
		if body.NextBefore == "" {
			break
		}
		if len(body.Messages) != 2 {
			t.Fatalf("has-more on short page %+v", idsOf(body.Messages))
		}
		before = body.NextBefore
	}
	if got := strings.Join(ids, ","); got != "m6,m5,m4,m3,m2,m1" {
		t.Fatalf("walked %s", got)
	}

	rec := do(t, h, http.MethodGet, "/v1/conversations/"+fixtures.FriendTalkerID+"/messages?account_id="+fixtures.AccountID+"&limit=20")
	var all msgPage
	decode(t, rec, &all)
	if len(all.Messages) != 6 {
		t.Fatalf("want 6 got %d", len(all.Messages))
	}
	if all.NextBefore != "" {
		t.Fatalf("unexpected next_before %q", all.NextBefore)
	}
}

func TestAccountIsolation(t *testing.T) {
	t.Parallel()
	store, dir := seededStore(t)
	if err := fixtures.SeedOther(context.Background(), store, dir); err != nil {
		t.Fatal(err)
	}
	h := handlerFor(t, store)

	rec := do(t, h, http.MethodGet, "/v1/conversations?account_id="+fixtures.OtherAccountID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.Bytes())
	}
	var convs struct {
		Conversations []domain.Conversation `json:"conversations"`
	}
	decode(t, rec, &convs)
	if len(convs.Conversations) != 2 {
		t.Fatalf("%+v", convs.Conversations)
	}
	for _, c := range convs.Conversations {
		if c.AccountID != fixtures.OtherAccountID {
			t.Fatalf("leaked conversation %+v", c)
		}
		if c.DisplayName == fixtures.FriendName || c.DisplayName == fixtures.GroupName {
			t.Fatalf("a1 conversation on a2: %+v", c)
		}
	}

	rec = do(t, h, http.MethodGet, "/v1/conversations/"+fixtures.FriendTalkerID+"/messages?account_id="+fixtures.OtherAccountID+"&limit=20")
	var other msgPage
	decode(t, rec, &other)
	if len(other.Messages) != 3 {
		t.Fatalf("a2 friend msgs %+v", idsOf(other.Messages))
	}
	for _, m := range other.Messages {
		if m.AccountID != fixtures.OtherAccountID || !strings.HasPrefix(m.Text, "other-") {
			t.Fatalf("leaked message %+v", m)
		}
	}

	rec = do(t, h, http.MethodGet, "/v1/conversations/"+fixtures.FriendTalkerID+"/messages?account_id="+fixtures.AccountID+"&limit=2")
	var a1page msgPage
	decode(t, rec, &a1page)
	if a1page.NextBefore == "" {
		t.Fatal("expected a1 cursor")
	}
	rec = do(t, h, http.MethodGet, "/v1/conversations/"+fixtures.FriendTalkerID+"/messages?account_id="+fixtures.OtherAccountID+
		"&limit=20&before="+url.QueryEscape(a1page.NextBefore))
	var crossed msgPage
	decode(t, rec, &crossed)
	for _, m := range crossed.Messages {
		if m.AccountID != fixtures.OtherAccountID || strings.Contains(m.Text, "fixture") || m.Text == "later text" {
			t.Fatalf("a1 cursor surfaced a1 message on a2: %+v", m)
		}
	}

	rec = do(t, h, http.MethodGet, "/v1/media/"+fixtures.AvailableMediaID+"?account_id="+fixtures.OtherAccountID)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("a2 media status %d body %s", rec.Code, rec.Body.Bytes())
	}
	assertErrorCode(t, rec, string(domain.CodeMediaNeverOpened))
	if rec.Body.String() == string(fixtures.AvailableMediaBytes) {
		t.Fatal("a2 received a1 media bytes")
	}

	rec = do(t, h, http.MethodGet, "/v1/media/"+fixtures.AvailableMediaID+"?account_id="+fixtures.AccountID)
	if rec.Code != http.StatusOK || rec.Body.String() != string(fixtures.AvailableMediaBytes) {
		t.Fatalf("a1 media %d %q", rec.Code, rec.Body.String())
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

func TestGetMediaMissingFile(t *testing.T) {
	t.Parallel()
	store, dir := seededStore(t)
	path := filepath.Join(dir, "accounts", fixtures.WxID, "media", "available.bin")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	h := handlerFor(t, store)
	rec := do(t, h, http.MethodGet, "/v1/media/"+fixtures.AvailableMediaID+"?account_id="+fixtures.AccountID)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.Bytes())
	}
	if rec.Body.Len() == 0 {
		t.Fatal("empty 404 body")
	}
	assertErrorCode(t, rec, string(domain.CodeMediaNeverOpened))
	var body errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code == "" || body.Error.Message == "" {
		t.Fatalf("incomplete error %+v", body.Error)
	}
}

func TestSearchKnownPhrase(t *testing.T) {
	t.Parallel()
	h := seededHandler(t)
	rec := do(t, h, http.MethodGet, "/v1/search?account_id="+fixtures.AccountID+"&q="+url.QueryEscape("hello fixture"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.Bytes())
	}
	var body msgPage
	decode(t, rec, &body)
	if len(body.Messages) != 1 || body.Messages[0].MsgID != "m1" || body.Messages[0].Text != "hello fixture" {
		t.Fatalf("%+v", body.Messages)
	}
}

func TestSearchTypeFilter(t *testing.T) {
	t.Parallel()
	h := seededHandler(t)
	rec := do(t, h, http.MethodGet, "/v1/search?account_id="+fixtures.AccountID+
		"&q=synthetic&msg_type="+strconv.Itoa(fixtures.MsgSystem))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.Bytes())
	}
	var body msgPage
	decode(t, rec, &body)
	if len(body.Messages) != 1 || body.Messages[0].MsgID != "m6" || body.Messages[0].MsgType != fixtures.MsgSystem {
		t.Fatalf("%+v", body.Messages)
	}

	rec = do(t, h, http.MethodGet, "/v1/search?account_id="+fixtures.AccountID+
		"&q=hello+fixture&msg_type="+strconv.Itoa(fixtures.MsgImage))
	decode(t, rec, &body)
	if rec.Code != http.StatusOK || len(body.Messages) != 0 {
		t.Fatalf("status %d %+v", rec.Code, body.Messages)
	}
}

func TestSearchEmptyQuery(t *testing.T) {
	t.Parallel()
	h := seededHandler(t)
	for _, path := range []string{
		"/v1/search?account_id=" + fixtures.AccountID,
		"/v1/search?account_id=" + fixtures.AccountID + "&q=",
		"/v1/search?account_id=" + fixtures.AccountID + "&q=%20",
	} {
		rec := do(t, h, http.MethodGet, path)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status %d body %s", path, rec.Code, rec.Body.Bytes())
		}
		if got := strings.TrimSpace(rec.Body.String()); got != `{"messages":[]}` {
			t.Fatalf("%s body %s", path, rec.Body.String())
		}
	}
}

func TestSearchRequiresAccount(t *testing.T) {
	t.Parallel()
	h := seededHandler(t)
	rec := do(t, h, http.MethodGet, "/v1/search?q=hello")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rec.Code)
	}
	assertErrorCode(t, rec, "invalid_request")
}

func TestSearchDateRangeAndBadParams(t *testing.T) {
	t.Parallel()
	h := seededHandler(t)
	from := time.Date(2024, 1, 2, 3, 4, 4, 0, time.UTC).UnixMilli()
	rec := do(t, h, http.MethodGet, "/v1/search?account_id="+fixtures.AccountID+"&q=later&from="+strconv.FormatInt(from, 10))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.Bytes())
	}
	var body msgPage
	decode(t, rec, &body)
	if len(body.Messages) != 1 || body.Messages[0].MsgID != "m5" {
		t.Fatalf("%+v", body.Messages)
	}

	rec = do(t, h, http.MethodGet, "/v1/search?account_id="+fixtures.AccountID+"&q=later&from=not-a-time")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rec.Code)
	}
	assertErrorCode(t, rec, "invalid_request")

	rec = do(t, h, http.MethodGet, "/v1/search?account_id="+fixtures.AccountID+"&q=later&msg_type=nope")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rec.Code)
	}
	assertErrorCode(t, rec, "invalid_request")
}

func TestSearchAccountIsolation(t *testing.T) {
	t.Parallel()
	store, dir := seededStore(t)
	if err := fixtures.SeedOther(context.Background(), store, dir); err != nil {
		t.Fatal(err)
	}
	h := handlerFor(t, store)

	rec := do(t, h, http.MethodGet, "/v1/search?account_id="+fixtures.AccountID+"&q=fixture")
	var a1 msgPage
	decode(t, rec, &a1)
	if len(a1.Messages) == 0 {
		t.Fatal("expected a1 hits")
	}
	for _, m := range a1.Messages {
		if m.AccountID != fixtures.AccountID || strings.HasPrefix(m.Text, "other-") {
			t.Fatalf("leaked %+v", m)
		}
	}

	rec = do(t, h, http.MethodGet, "/v1/search?account_id="+fixtures.OtherAccountID+"&q=other")
	var a2 msgPage
	decode(t, rec, &a2)
	if len(a2.Messages) == 0 {
		t.Fatal("expected a2 hits")
	}
	for _, m := range a2.Messages {
		if m.AccountID != fixtures.OtherAccountID || strings.Contains(m.Text, "fixture") {
			t.Fatalf("leaked %+v", m)
		}
	}

	rec = do(t, h, http.MethodGet, "/v1/search?account_id="+fixtures.OtherAccountID+"&q="+url.QueryEscape("hello fixture"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.Bytes())
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"messages":[]}` {
		t.Fatalf("a2 hello fixture %s", rec.Body.String())
	}
	rec = do(t, h, http.MethodGet, "/v1/search?account_id="+fixtures.AccountID+"&q=other")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.Bytes())
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"messages":[]}` {
		t.Fatalf("a1 other %s", rec.Body.String())
	}
}

func TestStatsFixture(t *testing.T) {
	t.Parallel()
	h := seededHandler(t)
	rec := do(t, h, http.MethodGet, "/v1/stats?account_id="+fixtures.AccountID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.Bytes())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type %q", ct)
	}
	var body statsBody
	decode(t, rec, &body)
	if body.AccountID != fixtures.AccountID || body.SessionCount != 2 || body.MessageCount != 8 {
		t.Fatalf("%+v", body)
	}
	want := []struct {
		MsgType int   `json:"msg_type"`
		Count   int64 `json:"count"`
	}{
		{MsgType: fixtures.MsgText, Count: 4},
		{MsgType: fixtures.MsgImage, Count: 1},
		{MsgType: fixtures.MsgVoice, Count: 1},
		{MsgType: fixtures.MsgVideo, Count: 1},
		{MsgType: fixtures.MsgSystem, Count: 1},
	}
	if len(body.Types) != len(want) {
		t.Fatalf("types %+v", body.Types)
	}
	for i, w := range want {
		if body.Types[i].MsgType != w.MsgType || body.Types[i].Count != w.Count {
			t.Fatalf("types %+v want %+v", body.Types, want)
		}
	}
}

func TestStatsRequiresAccount(t *testing.T) {
	t.Parallel()
	h := seededHandler(t)
	rec := do(t, h, http.MethodGet, "/v1/stats")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rec.Code)
	}
	assertErrorCode(t, rec, "invalid_request")
}

func TestStatsUnknownAccount(t *testing.T) {
	t.Parallel()
	h := seededHandler(t)
	rec := do(t, h, http.MethodGet, "/v1/stats?account_id=missing")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d", rec.Code)
	}
	assertErrorCode(t, rec, "not_found")
}

func TestStatsEmptyAccount(t *testing.T) {
	t.Parallel()
	store, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.PutAccount(context.Background(), domain.Account{
		ID: "empty", WxID: "wxid_empty", LoginState: domain.LoginStateLoggedIn,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.OpenAccount(context.Background(), "wxid_empty"); err != nil {
		t.Fatal(err)
	}
	h := handlerFor(t, store)
	rec := do(t, h, http.MethodGet, "/v1/stats?account_id=empty")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.Bytes())
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"account_id":"empty","session_count":0,"message_count":0,"types":[]}` {
		t.Fatalf("body %s", rec.Body.String())
	}
}

func TestStatsAccountIsolation(t *testing.T) {
	t.Parallel()
	store, dir := seededStore(t)
	if err := fixtures.SeedOther(context.Background(), store, dir); err != nil {
		t.Fatal(err)
	}
	h := handlerFor(t, store)

	rec := do(t, h, http.MethodGet, "/v1/stats?account_id="+fixtures.AccountID)
	var a1 statsBody
	decode(t, rec, &a1)
	if rec.Code != http.StatusOK || a1.AccountID != fixtures.AccountID || a1.MessageCount != 8 {
		t.Fatalf("a1 status %d %+v", rec.Code, a1)
	}

	rec = do(t, h, http.MethodGet, "/v1/stats?account_id="+fixtures.OtherAccountID)
	var a2 statsBody
	decode(t, rec, &a2)
	if rec.Code != http.StatusOK || a2.AccountID != fixtures.OtherAccountID || a2.SessionCount != 2 || a2.MessageCount != 4 {
		t.Fatalf("a2 status %d %+v", rec.Code, a2)
	}
	if len(a2.Types) != 1 || a2.Types[0].MsgType != fixtures.MsgText || a2.Types[0].Count != 4 {
		t.Fatalf("a2 types %+v", a2.Types)
	}
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

func TestGetMediaTranscodeMP3(t *testing.T) {
	t.Setenv(media.EnvFFmpeg, writeFFmpegStub(t))
	store, dir := seededStore(t)
	putVoice(t, store, dir, []byte("silk-bytes"))
	h := handlerFor(t, store)
	rec := do(t, h, http.MethodGet, "/v1/media/media_voice?account_id="+fixtures.AccountID+"&transcode=mp3")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.Bytes())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "audio/mpeg" {
		t.Fatalf("content-type %q", ct)
	}
	if rec.Body.String() != "STUB-mp3-silk-bytes" {
		t.Fatalf("body %q", rec.Body.String())
	}
}

func TestGetMediaTranscodeRequiresVoice(t *testing.T) {
	t.Setenv(media.EnvFFmpeg, writeFFmpegStub(t))
	h := seededHandler(t)
	rec := do(t, h, http.MethodGet, "/v1/media/"+fixtures.AvailableMediaID+"?account_id="+fixtures.AccountID+"&transcode=mp3")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.Bytes())
	}
}

func TestGetMediaTranscodeMissingFFmpeg(t *testing.T) {
	t.Setenv(media.EnvFFmpeg, filepath.Join(t.TempDir(), "no-ffmpeg"))
	store, dir := seededStore(t)
	putVoice(t, store, dir, []byte("silk"))
	h := handlerFor(t, store)
	rec := do(t, h, http.MethodGet, "/v1/media/media_voice?account_id="+fixtures.AccountID+"&transcode=mp3")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.Bytes())
	}
}

func putVoice(t *testing.T, store *sqlite.Store, dataDir string, body []byte) string {
	t.Helper()
	path := filepath.Join(dataDir, "accounts", fixtures.WxID, "media", "voice.bin")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	adb, err := store.OpenAccount(context.Background(), fixtures.WxID)
	if err != nil {
		t.Fatal(err)
	}
	if err := adb.PutMedia(context.Background(), domain.MediaObject{
		AccountID: fixtures.AccountID, MediaID: "media_voice", Kind: domain.MediaKindVoice,
		Path: path, Size: int64(len(body)), Available: true,
	}); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeFFmpegStub(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "ffmpeg")
	script := `#!/bin/sh
input=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "-i" ]; then
    input=$arg
  fi
  prev=$arg
done
output=$prev
fmt=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "-f" ]; then
    fmt=$arg
  fi
  prev=$arg
done
if [ -z "$input" ] || [ ! -f "$input" ]; then
  echo "ffmpeg-stub: input missing" >&2
  exit 1
fi
printf 'STUB-%s-' "$fmt" > "$output"
cat "$input" >> "$output"
`
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
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
	return handlerFor(t, store)
}

func handlerFor(t *testing.T, store *sqlite.Store) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	New(store).Register(mux)
	return mux
}

type msgPage struct {
	Messages   []domain.Message `json:"messages"`
	NextBefore string           `json:"next_before"`
}

type statsBody struct {
	AccountID    string `json:"account_id"`
	SessionCount int64  `json:"session_count"`
	MessageCount int64  `json:"message_count"`
	Types        []struct {
		MsgType int   `json:"msg_type"`
		Count   int64 `json:"count"`
	} `json:"types"`
}

func idsOf(msgs []domain.Message) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.MsgID
	}
	return out
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
