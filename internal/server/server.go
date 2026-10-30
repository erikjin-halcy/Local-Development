// Package server är devctl:s HTTP-API och webbgränssnitt. Lyssnar bara på 127.0.0.1.
package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/erikjin-halcy/Local-Development/internal/config"
	"github.com/erikjin-halcy/Local-Development/internal/control"
	"github.com/erikjin-halcy/Local-Development/internal/state"
	"github.com/erikjin-halcy/Local-Development/web"
)

// Header som krävs på POST-anrop: andra webbplatser kan inte sätta den utan CORS-preflight,
// som servern aldrig godkänner.
const Header = "X-Devctl"

func hostOK(r *http.Request) bool {
	host := r.Host
	if h, _, err := net.SplitHostPort(r.Host); err == nil {
		host = h
	}
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func Run(cfg config.Config) error {
	col := state.NewCollector(cfg)
	col.Start()
	ctl := control.New(cfg, col)

	if cfg.LogEveryMinutes > 0 && cfg.Obsidian.Vault != "" {
		go func() {
			for range time.Tick(time.Duration(cfg.LogEveryMinutes) * time.Minute) {
				if _, err := ctl.DailyLog(); err != nil {
					log.Println("daglig logg:", err)
				}
			}
		}()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if !hostOK(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(web.Index)
	})
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"ok": true, "port": cfg.Port})
	})
	mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		if !hostOK(r) {
			writeJSON(w, 403, map[string]string{"error": "forbidden host"})
			return
		}
		if r.URL.Query().Get("fresh") == "1" {
			writeJSON(w, 200, col.RefreshNow())
			return
		}
		writeJSON(w, 200, col.Snapshot())
	})
	mux.HandleFunc("/api/action", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !hostOK(r) || r.Header.Get(Header) != "1" {
			writeJSON(w, 403, map[string]string{"error": "forbidden"})
			return
		}
		var a control.Args
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&a); err != nil {
			writeJSON(w, 400, map[string]string{"error": "ogiltig JSON"})
			return
		}
		name, _ := a["action"].(string)
		res, err := ctl.Do(strings.TrimSpace(name), a)
		if err != nil {
			writeJSON(w, 400, map[string]any{"error": err.Error(), "result": res})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "result": res})
	})

	addr := fmt.Sprintf("127.0.0.1:%d", cfg.Port)
	log.Printf("devctl körs på http://localhost:%d", cfg.Port)
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	return srv.ListenAndServe()
}
