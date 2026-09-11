package capture

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/wxbackup/wxbackup/internal/domain"
)

var (
	// ErrNotAuthorized is returned when the operator has not granted the
	// own-phone + own-NAS capture scope. Default is denied.
	ErrNotAuthorized = errors.New("capture: LAN adapter needs a new authorized scope for the operator's own phone and NAS")
	// ErrNotImplemented is returned when the scope is granted but the
	// PCAP-driven state machine is still a stub (no fixtures, or fixtures
	// present without a parser implementation).
	ErrNotImplemented = errors.New("capture: backup from own-device PCAPs is not implemented")
)

// Scope is the extra grant required before this adapter may look at LAN
// frames. Zero value is denied.
type Scope struct {
	// OwnPhoneAndNAS is the only permitted capture surface: the operator's
	// phone and the operator's NAS on the same LAN. Production WeChat
	// servers and other people's devices stay out of scope.
	OwnPhoneAndNAS bool
}

// CaptureSession is a domain.DeviceSession that refuses to run a backup
// until own-device PCAP work is implemented. It never opens WeChat ports.
type CaptureSession struct {
	Scope Scope
	// FixtureDir is where operator-supplied pcap/pcapng files will live.
	// See README for how to add them; they are gitignored (real chat traffic).
	FixtureDir string
	// Parser is reserved for a future fixture-driven implementation.
	// StartBackup does not call it.
	Parser Parser
}

func New(scope Scope, fixtureDir string) *CaptureSession {
	return &CaptureSession{Scope: scope, FixtureDir: fixtureDir, Parser: StubParser{}}
}

var _ domain.DeviceSession = (*CaptureSession)(nil)

func (s *CaptureSession) refuse() error {
	if s == nil || !s.Scope.OwnPhoneAndNAS {
		return ErrNotAuthorized
	}
	return ErrNotImplemented
}

func (s *CaptureSession) LoginQR(context.Context) (domain.LoginSession, error) {
	return domain.LoginSession{}, s.refuse()
}

func (s *CaptureSession) WaitLoggedIn(context.Context) (domain.Account, error) {
	return domain.Account{}, s.refuse()
}

func (s *CaptureSession) StartBackup(context.Context, domain.BackupRequest) (domain.BackupStream, error) {
	// Never return a stream: a nil stream plus a sentinel is the guarantee
	// this stub does not start a LAN backup.
	return nil, s.refuse()
}

func (s *CaptureSession) StartRestore(context.Context, domain.RestoreRequest) (domain.RestoreStream, error) {
	return nil, s.refuse()
}

func (s *CaptureSession) RefreshContacts(context.Context) error { return s.refuse() }

func (s *CaptureSession) Close(context.Context) error { return nil }

// FixturesPresent reports whether FixtureDir contains pcap/pcapng files.
// Presence does not enable the protocol; StartBackup still refuses.
func (s *CaptureSession) FixturesPresent() bool {
	if s == nil {
		return false
	}
	return HasPCAPFixtures(s.FixtureDir)
}

// HasPCAPFixtures reports pcap/pcapng files under dir (non-recursive).
func HasPCAPFixtures(dir string) bool {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := strings.ToLower(e.Name())
		if strings.HasSuffix(name, ".pcap") || strings.HasSuffix(name, ".pcapng") {
			return true
		}
	}
	return false
}
