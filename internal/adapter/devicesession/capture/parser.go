package capture

import (
	"context"
	"io"
)

// Parser walks a user-owned PCAP into ExpectedPhases.
// Implementations must be derived from those captures, not from FPK
// protobuf copied out of a third-party binary.
type Parser interface {
	Parse(ctx context.Context, src Source) (*Trace, error)
}

// Source is a pcap/pcapng stream the operator captured on their LAN.
type Source struct {
	Path   string
	Reader io.Reader
}

// Trace is a phase list only. Payload bytes are not decoded in this stub.
type Trace struct {
	Phases []ObservedPhase
}

// ObservedPhase records that a named phase was seen, with the official
// port when known. It does not carry message bodies.
type ObservedPhase struct {
	Name Phase
	Port int
}

// StubParser is the only Parser in this PR. It never decodes frames.
type StubParser struct{}

func (StubParser) Parse(ctx context.Context, _ Source) (*Trace, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	return nil, ErrNotImplemented
}
