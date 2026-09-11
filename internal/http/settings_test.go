package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/wxbackup/wxbackup/internal/app/accesspwd"
	"github.com/wxbackup/wxbackup/internal/domain"
	"github.com/wxbackup/wxbackup/internal/store/sqlite"
	"github.com/wxbackup/wxbackup/testdata/fixtures"
	"golang.org/x/crypto/bcrypt"
)

func TestMain(m *testing.M) {
	accesspwd.Cost = bcrypt.MinCost
	os.Exit(m.Run())
}

func TestGetPutAccountSettings(t *testing.T) {
	t.Parallel()
	h := seededHandler(t)
	rec := do(t, h, http.MethodGet, "/v1/accounts/"+fixtures.AccountID+"/settings")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.Bytes())
	}
	raw := rec.Body.String()
	if strings.Contains(raw, "not-serialized") || strings.Contains(raw, "access_pwd") {
		t.Fatalf("password hash leaked: %s", raw)
	}
	var got accountSettings
	decode(t, rec, &got)
	if got.BackupRoot == "" || !got.HasPassword {
		t.Fatalf("%+v", got)
	}

	rec = doJSON(t, h, http.MethodPut, "/v1/accounts/"+fixtures.AccountID+"/settings", map[string]string{
		"backup_root": "/data/custom-root",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("put status %d body %s", rec.Code, rec.Body.Bytes())
	}
	var put accountSettings
	decode(t, rec, &put)
	if put.BackupRoot != "/data/custom-root" {
		t.Fatalf("%+v", put)
	}

	rec = do(t, h, http.MethodGet, "/v1/accounts/"+fixtures.AccountID+"/settings")
	var again accountSettings
	decode(t, rec, &again)
	if again.BackupRoot != "/data/custom-root" {
		t.Fatalf("%+v", again)
	}
}

func TestPasswordHashVerify(t *testing.T) {
	t.Parallel()
	store, _ := seededStore(t)
	h := handlerFor(t, store)

	rec := doJSON(t, h, http.MethodPut, "/v1/accounts/"+fixtures.AccountID+"/password", map[string]string{
		"password": "secret",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("put status %d body %s", rec.Code, rec.Body.Bytes())
	}

	acct, err := store.GetAccount(context.Background(), fixtures.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(acct.AccessPwdHash, "$2a$") && !strings.HasPrefix(acct.AccessPwdHash, "$2b$") {
		t.Fatalf("stored hash is not bcrypt: %s", acct.AccessPwdHash)
	}
	if !accesspwd.Verify(acct.AccessPwdHash, "secret") {
		t.Fatal("stored hash does not verify")
	}

	rec = do(t, h, http.MethodGet, "/v1/accounts")
	if strings.Contains(rec.Body.String(), acct.AccessPwdHash) {
		t.Fatal("bcrypt hash leaked in account list")
	}

	rec = doJSON(t, h, http.MethodPost, "/v1/accounts/"+fixtures.AccountID+"/password/verify", map[string]string{
		"password": "secret",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("verify status %d body %s", rec.Code, rec.Body.Bytes())
	}
	var okBody struct {
		OK bool `json:"ok"`
	}
	decode(t, rec, &okBody)
	if !okBody.OK {
		t.Fatal("expected ok")
	}

	rec = doJSON(t, h, http.MethodPost, "/v1/accounts/"+fixtures.AccountID+"/password/verify", map[string]string{
		"password": "wrong",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password status %d", rec.Code)
	}
	assertErrorCode(t, rec, string(domain.CodePasswordIncorrect))
}

func TestDeleteAccountWithoutPasswordRejected(t *testing.T) {
	t.Parallel()
	store, _ := seededStore(t)
	h := handlerFor(t, store)

	rec := doJSON(t, h, http.MethodPut, "/v1/accounts/"+fixtures.AccountID+"/password", map[string]string{
		"password": "secret",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("put status %d body %s", rec.Code, rec.Body.Bytes())
	}

	rec = do(t, h, http.MethodDelete, "/v1/accounts/"+fixtures.AccountID)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("delete without password status %d body %s", rec.Code, rec.Body.Bytes())
	}
	assertErrorCode(t, rec, string(domain.CodePasswordRequired))

	if _, err := store.GetAccount(context.Background(), fixtures.AccountID); err != nil {
		t.Fatalf("account should remain: %v", err)
	}

	rec = doJSON(t, h, http.MethodDelete, "/v1/accounts/"+fixtures.AccountID, map[string]string{
		"password": "wrong",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password status %d", rec.Code)
	}
	assertErrorCode(t, rec, string(domain.CodePasswordIncorrect))

	rec = doJSON(t, h, http.MethodDelete, "/v1/accounts/"+fixtures.AccountID, map[string]string{
		"password": "secret",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status %d body %s", rec.Code, rec.Body.Bytes())
	}
	if _, err := store.GetAccount(context.Background(), fixtures.AccountID); !errors.Is(err, sqlite.ErrNotFound) {
		t.Fatalf("deleted: %v", err)
	}
}

func TestDeleteAccountNoPasswordWhenUnset(t *testing.T) {
	t.Parallel()
	store, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	if err := store.PutAccount(ctx, domain.Account{
		ID: "plain", WxID: "wxid_plain", Nickname: "Plain",
		LoginState: domain.LoginStateLoggedOut,
	}); err != nil {
		t.Fatal(err)
	}
	h := handlerFor(t, store)
	rec := do(t, h, http.MethodDelete, "/v1/accounts/plain")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.Bytes())
	}
}

func TestSettingsUnknownAccount(t *testing.T) {
	t.Parallel()
	h := seededHandler(t)
	rec := do(t, h, http.MethodGet, "/v1/accounts/missing/settings")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d", rec.Code)
	}
	assertErrorCode(t, rec, "not_found")
}

func doJSON(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rdr = bytes.NewReader(b)
	}
	var req *http.Request
	if rdr != nil {
		req = httptest.NewRequest(method, path, rdr)
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}
