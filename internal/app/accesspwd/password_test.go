package accesspwd

import (
	"os"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestMain(m *testing.M) {
	Cost = bcrypt.MinCost
	os.Exit(m.Run())
}

func TestHashAndVerify(t *testing.T) {
	t.Parallel()
	hash, err := Hash("secret")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$2a$") && !strings.HasPrefix(hash, "$2b$") {
		t.Fatalf("not a bcrypt hash: %s", hash)
	}
	if !Verify(hash, "secret") {
		t.Fatal("correct password rejected")
	}
	if Verify(hash, "wrong") {
		t.Fatal("wrong password accepted")
	}
	if Verify(hash, "") {
		t.Fatal("empty password accepted")
	}
}

func TestVerifyEmptyHash(t *testing.T) {
	t.Parallel()
	if !Verify("", "") {
		t.Fatal("unset password should match empty")
	}
	if Verify("", "x") {
		t.Fatal("unset hash must not match a password")
	}
}
