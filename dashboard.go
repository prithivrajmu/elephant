package memory

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

//go:embed web/index.html
var dashboardHTML string

func DashboardHandler(s Service, host, token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if r.Host != host {
			http.Error(w, "invalid host", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+host {
			http.Error(w, "invalid origin", http.StatusForbidden)
			return
		}
		if r.URL.Path == "/" && r.Method == "GET" {
			nonce := newID()
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'nonce-"+nonce+"'; style-src 'unsafe-inline'; connect-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
			html := strings.ReplaceAll(dashboardHTML, "__NONCE__", nonce)
			html = strings.ReplaceAll(html, "__TOKEN__", token)
			fmt.Fprint(w, html)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		var v any
		var err error
		if r.URL.Path == "/api/dashboard" && r.Method == "GET" {
			var p Profile
			p, err = s.Profile()
			if err == nil {
				var d Dashboard
				d, err = s.Store.Dashboard(s.Identity, p)
				if err == nil {
					d.Language, err = s.Store.LanguagePolicy()
				}
				v = d
			}
		} else if r.URL.Path == "/api/language" && r.Method == "GET" {
			v, err = s.Store.LanguagePolicy()
		} else if r.URL.Path == "/api/map" && r.Method == "GET" {
			var p Profile
			p, err = s.Profile()
			if err == nil {
				var d Dashboard
				d, err = s.Store.Dashboard(s.Identity, p)
				v = BuildMemoryMap(d.Memories)
			}
		} else if r.URL.Path == "/api/language" && r.Method == "POST" {
			r.Body = http.MaxBytesReader(w, r.Body, 4096)
			var a struct {
				Target int `json:"target_percent"`
			}
			err = json.NewDecoder(r.Body).Decode(&a)
			if err == nil {
				v, err = s.Store.SetLanguagePolicy(a.Target)
			}
		} else if r.URL.Path == "/api/setup" && r.Method == "GET" {
			var binary string
			binary, err = os.Executable()
			if err == nil {
				v, err = SetupConfig(s, binary)
			}
		} else if r.Method == "POST" {
			r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
			var args json.RawMessage
			err = json.NewDecoder(r.Body).Decode(&args)
			name := map[string]string{"/api/recall": "recall_memory", "/api/record": "record_memory", "/api/feedback": "feedback_memory", "/api/forget": "forget_memory"}[r.URL.Path]
			if name == "" {
				http.NotFound(w, r)
				return
			}
			if err == nil {
				v, err = s.Call(name, args)
			}
		} else {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(v)
	})
}
func ServeDashboard(s Service, port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("port must be 1..65535")
	}
	host := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", host)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Elephant Memory Palace: http://%s\n", host)
	server := &http.Server{Handler: DashboardHandler(s, host, newID()), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second}
	return server.Serve(ln)
}
