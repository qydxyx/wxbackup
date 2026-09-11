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
	dist := lookupWebDist()
	if dist != "" {
		log.Printf("wxbackup %s listening on %s (data=%s web=%s)", Version, cfg.Addr(), cfg.DataDir, dist)
	} else {
		log.Printf("wxbackup %s listening on %s (data=%s web=off)", Version, cfg.Addr(), cfg.DataDir)
	}
	return http.ListenAndServe(cfg.Addr(), newMuxWithDist(Version, store, dist))
}

func newMux(version string, store *sqlite.Store) http.Handler {
	return newMuxWithDist(version, store, lookupWebDist())
}

func newMuxWithDist(version string, store *sqlite.Store, dist string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler(version))
	httpapi.New(store).Register(mux)
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
		return ""
	}
	cands := []string{"web/dist"}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		cands = append(cands, filepath.Join(dir, "web", "dist"), filepath.Join(dir, "dist"))
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
		if p != "/" && path.Ext(p) != "" {
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
