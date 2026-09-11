package config

import (
	"testing"

	"github.com/wxbackup/wxbackup/internal/domain"
)

func TestDefaultConfig(t *testing.T) {
	t.Parallel()
	cfg := Default()
	if cfg.ListenPort != domain.DefaultListenPort {
		t.Fatalf("port %d", cfg.ListenPort)
	}
	if cfg.DataDir != DefaultDataDir {
		t.Fatalf("data dir %q", cfg.DataDir)
	}
	if cfg.Addr() != ":20365" {
		t.Fatalf("addr %q", cfg.Addr())
	}
	d, tr := WeChatPorts()
	if d != 8011 || tr != 24011 {
		t.Fatalf("wechat ports %d %d", d, tr)
	}
}

func TestFromEnv(t *testing.T) {
	t.Setenv(EnvPort, "18080")
	t.Setenv(EnvData, "/tmp/wxbackup-test")
	cfg, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenPort != 18080 || cfg.DataDir != "/tmp/wxbackup-test" {
		t.Fatalf("%+v", cfg)
	}
}

func TestFromEnvInvalidPort(t *testing.T) {
	t.Setenv(EnvPort, "not-a-port")
	t.Setenv(EnvData, "data")
	if _, err := FromEnv(); err == nil {
		t.Fatal("expected error")
	}
	t.Setenv(EnvPort, "70000")
	if _, err := FromEnv(); err == nil {
		t.Fatal("expected range error")
	}
}

func TestFromEnvDefaults(t *testing.T) {
	t.Setenv(EnvPort, "")
	t.Setenv(EnvData, "")
	cfg, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenPort != 20365 || cfg.DataDir != "data" {
		t.Fatalf("%+v", cfg)
	}
}
