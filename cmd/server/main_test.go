package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wxbackup/wxbackup/internal/adapter/devicesession"
	"github.com/wxbackup/wxbackup/internal/app/backup"
	"github.com/wxbackup/wxbackup/internal/config"
	"github.com/wxbackup/wxbackup/internal/domain"
	"github.com/wxbackup/wxbackup/internal/store/sqlite"
)

func TestHealth(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(testMux(t, "testdev"))
	t.Cleanup(srv.Close)

	res, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type %q", ct)
	}
	var body struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "ok" || body.Version != "testdev" {
		t.Fatalf("%+v", body)
	}
}

func TestHealthMethodNotAllowed(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodPost, "/health", nil)
	rec := httptest.NewRecorder()
	testMux(t, "testdev").ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestSessionResolverFromConfig(t *testing.T) {
	t.Parallel()
	if devicesession.Resolver(config.Config{}.SidecarDir) != nil {
		t.Fatal("empty sidecar dir must leave session unconfigured")
	}
	dir := t.TempDir()
	r := devicesession.Resolver(config.Config{SidecarDir: dir}.SidecarDir)
	if r == nil {
		t.Fatal("expected sidecar resolver")
	}
	sess, err := r(context.Background(), domain.Account{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := sess.(*devicesession.Sidecar); !ok {
		t.Fatalf("got %T", sess)
	}
}

func TestViewerAccountsEmpty(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/v1/accounts", nil)
	rec := httptest.NewRecorder()
	testMux(t, "testdev").ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"accounts":[]}` {
		t.Fatalf("body %s", rec.Body.String())
	}
}

func TestViewerSearchRequiresAccount(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/v1/search?q=hello", nil)
	rec := httptest.NewRecorder()
	testMux(t, "testdev").ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestMissingWebDistAPIOnly(t *testing.T) {
	t.Parallel()
	h := testMux(t, "testdev")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("health %d", rec.Code)
	}
}

func TestSPAServesIndexAndAssets(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html><title>spa</title>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte("console.log(1)"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := testMuxDist(t, "testdev", dir)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "spa") {
		t.Fatalf("index %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/chat/a1", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "spa") {
		t.Fatalf("fallback %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/chat/a1/wxid.friend", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "spa") {
		t.Fatalf("dotted talker %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "console.log(1)" {
		t.Fatalf("asset %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/missing.js", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing asset %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/accounts", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("accounts %d", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"accounts":[]}` {
		t.Fatalf("body %s", rec.Body.String())
	}
}

func TestHasWebIndex(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if hasWebIndex(dir) || hasWebIndex("") {
		t.Fatal("expected missing index")
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !hasWebIndex(dir) {
		t.Fatal("expected index")
	}
}

func testMux(t *testing.T, version string) http.Handler {
	return testMuxDist(t, version, "")
}

func testMuxDist(t *testing.T, version, dist string) http.Handler {
	t.Helper()
	store, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc, err := backup.New(backup.Options{Store: store, DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	return newMuxWithDist(version, store, svc, dist)
}
