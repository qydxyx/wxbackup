package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/wxbackup/wxbackup/internal/config"
	httpapi "github.com/wxbackup/wxbackup/internal/http"
	"github.com/wxbackup/wxbackup/internal/store/sqlite"
)

// Version is overridden at build time with -ldflags.
var Version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "wxbackup: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	store, err := sqlite.Open(cfg.DataDir)
	if err != nil {
		return err
	}
	defer store.Close()
	log.Printf("wxbackup %s listening on %s (data=%s)", Version, cfg.Addr(), cfg.DataDir)
	return http.ListenAndServe(cfg.Addr(), newMux(Version, store))
}

func newMux(version string, store *sqlite.Store) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler(version))
	httpapi.New(store).Register(mux)
	return mux
}

func healthHandler(version string) http.HandlerFunc {
	type body struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body{Status: "ok", Version: version})
	}
}
