package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/wxbackup/wxbackup/internal/domain"
)

const (
	EnvPort        = "WXBACKUP_PORT"
	EnvData        = "WXBACKUP_DATA"
	DefaultDataDir = "data"
)

// Config is process-level runtime settings. WeChat discovery/transfer ports
// are not fields: they are fixed at domain.WeChatDiscoveryPort / WeChatTransferPort.
type Config struct {
	ListenPort int
	DataDir    string
}

func Default() Config {
	return Config{
		ListenPort: domain.DefaultListenPort,
		DataDir:    DefaultDataDir,
	}
}

func FromEnv() (Config, error) {
	cfg := Default()
	if v := strings.TrimSpace(os.Getenv(EnvPort)); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			return Config{}, fmt.Errorf("%s: invalid port %q", EnvPort, v)
		}
		if p < 1 || p > 65535 {
			return Config{}, fmt.Errorf("%s: port %d out of range", EnvPort, p)
		}
		cfg.ListenPort = p
	}
	if v := strings.TrimSpace(os.Getenv(EnvData)); v != "" {
		cfg.DataDir = v
	}
	if cfg.DataDir == "" {
		return Config{}, fmt.Errorf("%s: data directory is required", EnvData)
	}
	return cfg, nil
}

func (c Config) Addr() string {
	return fmt.Sprintf(":%d", c.ListenPort)
}

func WeChatPorts() (discovery, transfer int) {
	return domain.WeChatDiscoveryPort, domain.WeChatTransferPort
}
