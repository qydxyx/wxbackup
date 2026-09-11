package capture

import "github.com/wxbackup/wxbackup/internal/domain"

// Phase is a named step in the phone↔NAS LAN backup flow.
// Official ports are listed; frame layouts and protobuf are not.
type Phase string

const (
	// PhaseDiscovery is reachability on the official WeChat pair
	// (8011 discovery, 24011 transfer). The phone will not use other ports.
	PhaseDiscovery Phase = "discovery"
	// PhaseTransfer is the session data plane on the transfer port.
	PhaseTransfer Phase = "transfer"
	// PhaseHeartbeat is keepalive while a backup is in progress.
	PhaseHeartbeat Phase = "heartbeat"
)

// ExpectedPhases is the order a future own-device PCAP parser should report.
// No protobuf field numbers or third-party message names belong here.
var ExpectedPhases = []Phase{
	PhaseDiscovery,
	PhaseTransfer,
	PhaseHeartbeat,
}

func PhasePorts() (discovery, transfer int) {
	return domain.WeChatDiscoveryPort, domain.WeChatTransferPort
}
