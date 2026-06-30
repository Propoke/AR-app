// Command server runs the AR Remote Support backend: identity, session/token,
// and signaling services as a single process (modular monolith).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/propoke/ar-app/backend/internal/auth"
	"github.com/propoke/ar-app/backend/internal/config"
	"github.com/propoke/ar-app/backend/internal/httpapi"
	"github.com/propoke/ar-app/backend/internal/identity"
	"github.com/propoke/ar-app/backend/internal/metrics"
	"github.com/propoke/ar-app/backend/internal/ratelimit"
	"github.com/propoke/ar-app/backend/internal/recordings"
	"github.com/propoke/ar-app/backend/internal/session"
	"github.com/propoke/ar-app/backend/internal/signaling"
	"github.com/propoke/ar-app/backend/internal/store"
	"github.com/propoke/ar-app/backend/internal/turn"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if err := run(logger); err != nil {
		logger.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, cfg.DatabaseURL, cfg.RedisURL)
	if err != nil {
		return err
	}
	defer st.Close()

	if err := st.Migrate(ctx); err != nil {
		return err
	}
	logger.Info("migrations applied")

	issuer := auth.NewIssuer(cfg.JWTSecret, cfg.AccessTokenTTL, cfg.RefreshTokenTTL, cfg.SignalingTokenTTL)
	turnMinter := turn.NewMinter(cfg.TURNSecret, cfg.TURNURLs, cfg.TURNCredTTL)

	identitySvc := identity.NewService(st.DB)
	sessionSvc := session.NewService(st.DB, st.Redis, turnMinter, issuer, cfg.SessionTokenTTL)
	hub := signaling.NewHub()

	// Enforce per-session signaling join tokens: the token must authorize the
	// exact room and role the peer is connecting as.
	signalingAuth := func(room, role, token string) error {
		claims, err := issuer.ParseSignaling(token)
		if err != nil {
			return err
		}
		if claims.Room != room || claims.Role != role {
			return errors.New("token does not match room/role")
		}
		return nil
	}

	// Optional session recordings (enabled when object storage is configured).
	var recordingHandlers *recordings.Handlers
	if cfg.RecordingsEnabled() {
		presigner, err := recordings.NewS3Presigner(
			cfg.S3Endpoint, cfg.S3AccessKey, cfg.S3SecretKey, cfg.S3Bucket, cfg.S3Region, cfg.S3UseSSL)
		if err != nil {
			return fmt.Errorf("object storage: %w", err)
		}
		recordingSvc := recordings.NewService(st.DB, presigner, cfg.RecordingURLTTL)
		recordingHandlers = recordings.NewHandlers(recordingSvc)
		logger.Info("session recordings enabled", "bucket", cfg.S3Bucket)
	}

	handler := httpapi.New(httpapi.Deps{
		Identity:      identity.NewHandlers(identitySvc, issuer),
		Session:       session.NewHandlers(sessionSvc),
		Signaling:     signaling.NewHandler(hub, logger, signalingAuth),
		Issuer:        issuer,
		Logger:        logger,
		Readiness:     readinessHandler(st),
		AuthLimiter:   ratelimit.New(ratelimit.NewRedisStore(st.Redis), 10, time.Minute, logger),
		RedeemLimiter: ratelimit.New(ratelimit.NewRedisStore(st.Redis), 20, time.Minute, logger),
		Recordings:    recordingHandlers,
	})

	// Sample the active signaling room count into the Prometheus gauge.
	go sampleRooms(ctx, hub)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		// No global WriteTimeout: the signaling endpoint holds long-lived WebSockets.
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
		logger.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

// readinessHandler reports 200 only when Postgres and Redis are both reachable.
func readinessHandler(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := st.DB.Ping(ctx); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		if err := st.Redis.Ping(ctx).Err(); err != nil {
			http.Error(w, "redis unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	}
}

// sampleRooms periodically publishes the active room count to the metrics gauge.
func sampleRooms(ctx context.Context, hub *signaling.Hub) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			metrics.SignalingRooms.Set(float64(hub.RoomCount()))
		}
	}
}
