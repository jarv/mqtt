package main

import (
	"context"
	"embed"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

const oneYearCacheControl = "public, max-age=31536000"

var (
	//go:embed dist/*
	distFiles embed.FS

	//go:embed tmpl/*.tmpl
	tmplFiles embed.FS

	templates = template.Must(template.ParseFS(tmplFiles, "tmpl/*.tmpl"))
	cacheBust = time.Now().Format("20060102150405")
)

type App struct {
	cm         *ConnectionManager
	subscriber *Subscriber
	addr       string
}

func NewApp(addr string, cm *ConnectionManager, sub *Subscriber) *App {
	return &App{addr: addr, cm: cm, subscriber: sub}
}

func (a *App) Run() error {
	mux := http.NewServeMux()

	// Static assets
	distFS, _ := fs.Sub(distFiles, "dist")
	mux.Handle("GET /static/", cacheControlMiddleware(
		http.StripPrefix("/static/", http.FileServer(http.FS(distFS))),
		oneYearCacheControl,
	))

	// WebSocket
	mux.HandleFunc("GET /ws", a.handleWebSocket)

	// Index
	mux.HandleFunc("/", a.handleIndex)

	server := &http.Server{
		Addr:              a.addr,
		ReadHeaderTimeout: 3 * time.Second,
		Handler:           mux,
	}

	slog.Info("HTTP server started", "addr", "http://"+a.addr)
	return server.ListenAndServe()
}

func (a *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	d := struct{ CacheBust string }{CacheBust: cacheBust}
	if err := templates.ExecuteTemplate(w, "index.html.tmpl", d); err != nil {
		slog.Error("template error", "err", err)
		http.Error(w, "server error", http.StatusInternalServerError)
	}
}

func (a *App) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		slog.Error("WebSocket accept failed", "err", err)
		return
	}
	defer func() {
		_ = conn.CloseNow()
	}()

	clientID := r.Header.Get("X-Forwarded-For")
	if clientID == "" {
		clientID = r.RemoteAddr
	}

	a.cm.Add("browsers", conn)
	defer a.cm.Remove("browsers", conn)

	slog.Info("WebSocket connected", "client", clientID, "total", a.cm.Count())

	// Send current device snapshot to the newly connected client.
	ctx := r.Context()
	snapshot, err := a.subscriber.LoadAndBroadcast(ctx)
	if err != nil {
		slog.Error("failed to load initial devices", "err", err)
	} else {
		writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		if err := conn.Write(writeCtx, websocket.MessageText, snapshot); err != nil {
			slog.Warn("failed to send initial snapshot", "err", err)
		}
		cancel()
	}

	// Keep connection alive; read and discard messages.
	for {
		readCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		_, _, err := conn.Read(readCtx)
		cancel()
		if err != nil {
			slog.Info("WebSocket disconnected", "client", clientID)
			return
		}
	}
}

func cacheControlMiddleware(next http.Handler, cacheControl string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", cacheControl)
		next.ServeHTTP(w, r)
	})
}
