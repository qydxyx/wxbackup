package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/wxbackup/wxbackup/internal/adapter/devicesession"
	"github.com/wxbackup/wxbackup/internal/app/backup"
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
	svc, err := backup.New(backup.Options{
		Store:    store,
		DataDir:  cfg.DataDir,
		Sessions: devicesession.Resolver(cfg.SidecarDir),
	})
	if err != nil {
		return err
	}
	defer svc.Close()
	log.Printf("wxbackup %s listening on %s (data=%s)", Version, cfg.Addr(), cfg.DataDir)
	return http.ListenAndServe(cfg.Addr(), newMux(Version, store, svc))
}

func newMux(version string, store *sqlite.Store, svc *backup.Service) http.Handler {
	dist := lookupWebDist()
	if dist != "" {
		log.Printf("serving web UI from %s", dist)
	}
	return newMuxWithDist(version, store, svc, dist)
}

func newMuxWithDist(version string, store *sqlite.Store, svc *backup.Service, dist string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler(version))
	httpapi.New(store).Register(mux)
	if svc != nil {
		svc.Register(mux)
	}
	if dist != "" {
		mux.Handle("/", spaHandler(dist))
	}
	return mux
}

func lookupWebDist() string {
	if v := strings.TrimSpace(os.Getenv("WXBACKUP_WEB")); v != "" {
		if hasWebIndex(v) {
			return v
		}
		log.Printf("WXBACKUP_WEB=%s has no index.html; web=off", v)
		return ""
	}
	cands := []string{"web/dist", "web/dist-port"}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		cands = append(cands,
			filepath.Join(dir, "web", "dist"),
			filepath.Join(dir, "web", "dist-port"),
			filepath.Join(dir, "dist"),
		)
	}
	for _, d := range cands {
		if hasWebIndex(d) {
			return d
		}
	}
	return ""
}

func hasWebIndex(dir string) bool {
	if dir == "" {
		return false
	}
	st, err := os.Stat(filepath.Join(dir, "index.html"))
	return err == nil && !st.IsDir()
}

func spaHandler(dist string) http.Handler {
	root := http.Dir(dist)
	files := http.FileServer(root)
	index := filepath.Join(dist, "index.html")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}
		p := path.Clean(r.URL.Path)
		if isHashedAsset(p) {
			f, err := root.Open(p)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			st, err := f.Stat()
			_ = f.Close()
			if err != nil || st.IsDir() {
				http.NotFound(w, r)
				return
			}
			files.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, index)
	})
}

func isHashedAsset(p string) bool {
	p = path.Clean("/" + strings.TrimPrefix(p, "/"))
	return p == "/assets" || strings.HasPrefix(p, "/assets/") || strings.Contains(p, "/assets/")
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
