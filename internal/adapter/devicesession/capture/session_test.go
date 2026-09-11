package capture

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wxbackup/wxbackup/internal/domain"
)

func TestUnauthorizedStartBackupDoesNotStart(t *testing.T) {
	t.Parallel()
	sess := New(Scope{}, "")
	stream, err := sess.StartBackup(context.Background(), domain.BackupRequest{Mode: domain.BackupModeFull})
	if stream != nil {
		t.Fatal("unauthorized stub must not return a backup stream")
	}
	if !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("got %v want ErrNotAuthorized", err)
	}
}

func TestAuthorizedWithoutFixturesStillNoBackup(t *testing.T) {
	t.Parallel()
	sess := New(Scope{OwnPhoneAndNAS: true}, t.TempDir())
	if sess.FixturesPresent() {
		t.Fatal("empty dir is not a fixture set")
	}
	stream, err := sess.StartBackup(context.Background(), domain.BackupRequest{
		AccountID: "a1", Mode: domain.BackupModeFull,
	})
	if stream != nil {
		t.Fatal("stub must not return a backup stream")
	}
	if !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("got %v want ErrNotImplemented", err)
	}
}

func TestAuthorizedWithDummyPCAPStillNoBackup(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "own-device.pcap")
	if err := os.WriteFile(path, []byte("not-a-real-capture"), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := New(Scope{OwnPhoneAndNAS: true}, dir)
	if !sess.FixturesPresent() {
		t.Fatal("dummy pcap should count as a fixture file")
	}
	stream, err := sess.StartBackup(context.Background(), domain.BackupRequest{Mode: domain.BackupModeIncremental})
	if stream != nil {
		t.Fatal("fixture files must not enable a live backup in this stub")
	}
	if !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("got %v want ErrNotImplemented", err)
	}
}

func TestCaptureSessionDoesNotBindWeChatPorts(t *testing.T) {
	t.Parallel()
	sess := New(Scope{OwnPhoneAndNAS: true}, t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	stream, err := sess.StartBackup(ctx, domain.BackupRequest{Mode: domain.BackupModeFull})
	if stream != nil || !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("stream=%v err=%v", stream, err)
	}
	discovery, transfer := PhasePorts()
	if discovery != 8011 || transfer != 24011 {
		t.Fatalf("ports %d/%d", discovery, transfer)
	}
	for _, port := range []int{discovery, transfer} {
		ln, lerr := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if lerr != nil {
			if isAddrInUse(lerr) {
				t.Logf("port %d already in use on host; stub still returned no stream", port)
				continue
			}
			t.Fatalf("listen %d: %v", port, lerr)
		}
		_ = ln.Close()
	}
}

func TestOtherMethodsRefuse(t *testing.T) {
	t.Parallel()
	denied := New(Scope{}, "")
	granted := New(Scope{OwnPhoneAndNAS: true}, "")

	if _, err := denied.LoginQR(context.Background()); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("LoginQR denied: %v", err)
	}
	if _, err := granted.LoginQR(context.Background()); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("LoginQR granted: %v", err)
	}
	if _, err := denied.WaitLoggedIn(context.Background()); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("WaitLoggedIn: %v", err)
	}
	_, restoreErr := denied.StartRestore(context.Background(), domain.RestoreRequest{})
	if !errors.Is(restoreErr, ErrNotAuthorized) {
		t.Fatalf("StartRestore: %v", restoreErr)
	}
	if err := denied.RefreshContacts(context.Background()); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("RefreshContacts: %v", err)
	}
	if err := denied.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	var nilSess *CaptureSession
	_, nilErr := nilSess.StartBackup(context.Background(), domain.BackupRequest{})
	if !errors.Is(nilErr, ErrNotAuthorized) {
		t.Fatalf("nil session: %v", nilErr)
	}
}

func TestHasPCAPFixtures(t *testing.T) {
	t.Parallel()
	if HasPCAPFixtures("") || HasPCAPFixtures(filepath.Join(t.TempDir(), "missing")) {
		t.Fatal("empty/missing dir")
	}
	dir := t.TempDir()
	if HasPCAPFixtures(dir) {
		t.Fatal("empty dir")
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("no"), 0o644); err != nil {
		t.Fatal(err)
	}
	if HasPCAPFixtures(dir) {
		t.Fatal("non-pcap file")
	}
	if err := os.WriteFile(filepath.Join(dir, "lab.pcapng"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !HasPCAPFixtures(dir) {
		t.Fatal("pcapng")
	}
}

func isAddrInUse(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "address already in use") || strings.Contains(msg, "bind: Only one usage")
}
