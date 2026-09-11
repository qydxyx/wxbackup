package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wxbackup/wxbackup/internal/domain"
	"github.com/wxbackup/wxbackup/internal/store/sqlite"
)

func TestHTTPStartGetCancel(t *testing.T) {
	t.Parallel()
	s, fake, acct := setup(t, nil)
	hold, stop := context.WithCancel(context.Background())
	defer stop()
	fake.Hold = hold
	fake.Holding = make(chan struct{})
	fake.Chunks = []Chunk{friendChunk(acct.ID, "wxid_friend", time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), "hi", 2)}

	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)

	res, err := http.Post(srv.URL+"/v1/accounts/"+acct.ID+"/backup", "application/json", strings.NewReader(`{"mode":"full"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("status %d", res.StatusCode)
	}
	var job domain.BackupJob
	if err := json.NewDecoder(res.Body).Decode(&job); err != nil {
		t.Fatal(err)
	}
	if job.ID == "" || job.Status != domain.JobQueued && job.Status != domain.JobTransfer {
		t.Fatalf("%+v", job)
	}

	select {
	case <-fake.Holding:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for transfer hold")
	}

	get, err := http.Get(srv.URL + "/v1/accounts/" + acct.ID + "/backup/" + job.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer get.Body.Close()
	if get.StatusCode != http.StatusOK {
		t.Fatalf("get %d", get.StatusCode)
	}
	var got domain.BackupJob
	if err := json.NewDecoder(get.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.ID != job.ID || got.AccountID != acct.ID {
		t.Fatalf("%+v", got)
	}

	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/v1/accounts/"+acct.ID+"/backup/"+job.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	del, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer del.Body.Close()
	if del.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(del.Body)
		t.Fatalf("delete %d %s", del.StatusCode, body)
	}
	var cancelled domain.BackupJob
	if err := json.NewDecoder(del.Body).Decode(&cancelled); err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != domain.JobCancelled {
		t.Fatalf("%+v", cancelled)
	}
}

func TestHTTPSSEProgress(t *testing.T) {
	t.Parallel()
	s, fake, acct := setup(t, nil)
	fake.Chunks = []Chunk{friendChunk(acct.ID, "wxid_friend", time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC), "sse", 5)}

	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)

	res, err := http.Post(srv.URL+"/v1/accounts/"+acct.ID+"/backup", "application/json", strings.NewReader(`{"mode":"full"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var job domain.BackupJob
	if err := json.NewDecoder(res.Body).Decode(&job); err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/v1/accounts/"+acct.ID+"/backup/"+job.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", "text/event-stream")
	sse, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer sse.Body.Close()
	if ct := sse.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("content-type %q", ct)
	}
	body, err := io.ReadAll(sse.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte(`"status":"done"`)) && !bytes.Contains(body, []byte(`"status": "done"`)) {
		t.Fatalf("sse body %s", body)
	}
}

func TestHTTPErrors(t *testing.T) {
	t.Parallel()
	s, _, acct := setup(t, nil)
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)

	res, err := http.Post(srv.URL+"/v1/accounts/"+acct.ID+"/backup", "application/json", strings.NewReader(`{"mode":"delta"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad mode %d", res.StatusCode)
	}

	missing, err := http.Get(srv.URL + "/v1/accounts/" + acct.ID + "/backup/nope")
	if err != nil {
		t.Fatal(err)
	}
	defer missing.Body.Close()
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("missing job %d", missing.StatusCode)
	}

	dir := t.TempDir()
	store, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.PutAccount(context.Background(), domain.Account{
		ID: "a1", WxID: "wxid_fixture", LoginState: domain.LoginStateLoggedIn,
	}); err != nil {
		t.Fatal(err)
	}
	bare, err := New(Options{Store: store, DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bare.Close() })
	bareSrv := httptest.NewServer(bare.Handler())
	t.Cleanup(bareSrv.Close)
	noSess, err := http.Post(bareSrv.URL+"/v1/accounts/a1/backup", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer noSess.Body.Close()
	if noSess.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("no session %d", noSess.StatusCode)
	}
}

func TestHTTPCancelAfterDoneConflict(t *testing.T) {
	t.Parallel()
	s, fake, acct := setup(t, nil)
	fake.Chunks = []Chunk{friendChunk(acct.ID, "wxid_friend", time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC), "done", 1)}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	res, err := http.Post(srv.URL+"/v1/accounts/"+acct.ID+"/backup", "application/json", strings.NewReader(`{"mode":"full"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var job domain.BackupJob
	if err := json.NewDecoder(res.Body).Decode(&job); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Wait(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/v1/accounts/"+acct.ID+"/backup/"+job.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	del, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer del.Body.Close()
	if del.StatusCode != http.StatusConflict {
		body, _ := io.ReadAll(del.Body)
		t.Fatalf("delete %d %s", del.StatusCode, body)
	}
}
