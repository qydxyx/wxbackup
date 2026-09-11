package capture

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestStubParserDoesNotDecode(t *testing.T) {
	t.Parallel()
	trace, err := StubParser{}.Parse(context.Background(), Source{Path: "own.pcap", Reader: strings.NewReader("pcap")})
	if trace != nil {
		t.Fatal("stub parser must not return a trace")
	}
	if !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("got %v", err)
	}
}

func TestStubParserHonorsCancel(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := StubParser{}.Parse(ctx, Source{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

func TestExpectedPhases(t *testing.T) {
	t.Parallel()
	want := []Phase{PhaseDiscovery, PhaseTransfer, PhaseHeartbeat}
	if len(ExpectedPhases) != len(want) {
		t.Fatalf("got %v", ExpectedPhases)
	}
	for i := range want {
		if ExpectedPhases[i] != want[i] {
			t.Fatalf("index %d: %s", i, ExpectedPhases[i])
		}
	}
	d, x := PhasePorts()
	if d != 8011 || x != 24011 {
		t.Fatalf("ports %d/%d", d, x)
	}
	if PhaseDiscovery != "discovery" || PhaseTransfer != "transfer" || PhaseHeartbeat != "heartbeat" {
		t.Fatal("phase names")
	}
}
