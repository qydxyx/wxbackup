package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

func testMux(t *testing.T, version string) http.Handler {
	t.Helper()
	store, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return newMux(version, store)
}
