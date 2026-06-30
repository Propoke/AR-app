// Command signaling-dev runs only the signaling WebSocket relay, with no Postgres
// or Redis dependency. It exists so the WebRTC handshake can be exercised in
// isolation — by the Go relay test and the Node interop harness — without standing
// up the full backend. Do not use it in production; the full server is cmd/server.
package main

import (
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/propoke/ar-app/backend/internal/signaling"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8081"
	}

	hub := signaling.NewHub()
	// Dev relay: no token enforcement (nil authorizer).
	handler := signaling.NewHandler(hub, logger, nil)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /v1/signaling", handler.ServeWS)

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	logger.Info("signaling-dev listening", "addr", addr)
	if err := srv.ListenAndServe(); err != nil {
		logger.Error("server stopped", "err", err)
		os.Exit(1)
	}
}
