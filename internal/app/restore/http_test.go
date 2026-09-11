package restore

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wxbackup/wxbackup/internal/adapter/backupfmt"
	"github.com/wxbackup/wxbackup/internal/domain"
)

func TestHTTPRestoreStartGet(t *testing.T) {
	t.Parallel()
	s, _, acct := setup(t)
	seedTwoTalkers(t, s.store, acct)
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)

	res, err := http.Post(srv.URL+"/v1/accounts/"+acct.ID+"/restore", "application/json", strings.NewReader(`{"selector":"all"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("status %d %s", res.StatusCode, body)
	}
	var job domain.RestoreJob
	if err := json.NewDecoder(res.Body).Decode(&job); err != nil {
		t.Fatal(err)
	}
	if job.ID == "" || job.Selector.Kind != domain.RestoreAll {
		t.Fatalf("%+v", job)
	}
	if _, err := s.Wait(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}

	get, err := http.Get(srv.URL + "/v1/accounts/" + acct.ID + "/restore/" + job.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer get.Body.Close()
	if get.StatusCode != http.StatusOK {
		t.Fatalf("get %d", get.StatusCode)
	}
	var got domain.RestoreJob
	if err := json.NewDecoder(get.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.ID != job.ID || got.Status != domain.JobDone || got.SessionsDone != 2 {
		t.Fatalf("%+v", got)
	}

	partial, err := http.Post(srv.URL+"/v1/accounts/"+acct.ID+"/restore", "application/json",
		strings.NewReader(`{"selector":"session_ids","session_ids":["wxid_friend"]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer partial.Body.Close()
	if partial.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(partial.Body)
		t.Fatalf("partial %d %s", partial.StatusCode, body)
	}
	var job2 domain.RestoreJob
	if err := json.NewDecoder(partial.Body).Decode(&job2); err != nil {
		t.Fatal(err)
	}
	if done, err := s.Wait(context.Background(), job2.ID); err != nil || done.Status != domain.JobDone {
		t.Fatalf("%+v %v", done, err)
	}
}

func TestHTTPRestoreErrors(t *testing.T) {
	t.Parallel()
	s, _, acct := setup(t)
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)

	res, err := http.Post(srv.URL+"/v1/accounts/"+acct.ID+"/restore", "application/json", strings.NewReader(`{"selector":"none"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad selector %d", res.StatusCode)
	}

	emptyIDs, err := http.Post(srv.URL+"/v1/accounts/"+acct.ID+"/restore", "application/json",
		strings.NewReader(`{"selector":"session_ids"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer emptyIDs.Body.Close()
	if emptyIDs.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty session_ids %d", emptyIDs.StatusCode)
	}

	missing, err := http.Get(srv.URL + "/v1/accounts/" + acct.ID + "/restore/nope")
	if err != nil {
		t.Fatal(err)
	}
	defer missing.Body.Close()
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("missing job %d", missing.StatusCode)
	}

	bare := httptest.NewServer(mustBare(t).Handler())
	t.Cleanup(bare.Close)
	noSess, err := http.Post(bare.URL+"/v1/accounts/a1/restore", "application/json", strings.NewReader(`{"selector":"all"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer noSess.Body.Close()
	if noSess.StatusCode != http.StatusServiceUnavailable {
		body, _ := io.ReadAll(noSess.Body)
		t.Fatalf("no session %d %s", noSess.StatusCode, body)
	}
}

func TestHTTPExport(t *testing.T) {
	t.Parallel()
	s, _, acct := setup(t)
	seedTwoTalkers(t, s.store, acct)
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	body := `{"dir":` + jsonString(dir) + `,"selector":"all"}`
	res, err := http.Post(srv.URL+"/v1/accounts/"+acct.ID+"/export", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(res.Body)
		t.Fatalf("status %d %s", res.StatusCode, raw)
	}
	var got ExportResult
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Dir != dir {
		t.Fatalf("%+v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, backupfmt.BackupDBName)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "BAK_0_TEXT")); err != nil {
		t.Fatal(err)
	}

	bad, err := http.Post(srv.URL+"/v1/accounts/"+acct.ID+"/export", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer bad.Body.Close()
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty dir %d", bad.StatusCode)
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func mustBare(t *testing.T) *Service {
	t.Helper()
	s, _, _ := setup(t)
	bare, err := New(Options{Store: s.store, DataDir: s.dataDir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bare.Close() })
	return bare
}
