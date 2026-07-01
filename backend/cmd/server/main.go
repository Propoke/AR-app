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

	"github.com/google/uuid"

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

	// X-Forwarded-For is only honored from these addresses (e.g. Caddy's) when
	// deriving a client IP for rate limiting; empty by default (see config).
	trustedProxies, err := ratelimit.ParseTrustedProxies(cfg.TrustedProxyCIDRs)
	if err != nil {
		return fmt.Errorf("TRUSTED_PROXY_CIDRS: %w", err)
	}

	identitySvc := identity.NewService(st.DB)
	sessionSvc := session.NewService(st.DB, st.Redis, turnMinter, issuer, cfg.SessionTokenTTL)

	// Signaling delivery: Redis pub/sub across replicas, or in-process by default.
	var hub *signaling.Hub
	if cfg.SignalingFanout == "redis" {
		fanout := signaling.NewRedisFanout(st.Redis, logger)
		presence := signaling.NewRedisPresence(st.Redis, cfg.SignalingTokenTTL)
		hub = signaling.NewHubWith(fanout, presence)
		logger.Info("signaling fanout", "mode", "redis")
	} else {
		hub = signaling.NewHub()
	}

	// Mark a session ended in the database once its signaling room has no
	// occupants left on any instance (see signaling.Hub.OnRoomEnded). Room ids
	// are always session UUIDs (set at mint/redeem time), so this should never
	// fail to parse; if it somehow did, there's nothing sensible to do but log.
	hub.OnRoomEnded(func(room string) {
		sessionID, err := uuid.Parse(room)
		if err != nil {
			logger.Warn("room-ended callback: room is not a session id", "room", room, "err", err)
			return
		}
		endCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := sessionSvc.MarkEnded(endCtx, sessionID); err != nil {
			logger.Warn("mark session ended failed", "session_id", sessionID, "err", err)
		}
	})

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

	rlStore := ratelimit.NewRedisStore(st.Redis)
	handler := httpapi.New(httpapi.Deps{
		Identity:        identity.NewHandlers(identitySvc, issuer),
		Session:         session.NewHandlers(sessionSvc),
		Signaling:       signaling.NewHandler(hub, logger, signalingAuth),
		Issuer:          issuer,
		Logger:          logger,
		Readiness:       readinessHandler(st),
		RegisterLimiter: ratelimit.New(rlStore, 5, time.Minute, logger, trustedProxies),
		AuthLimiter:     ratelimit.New(rlStore, 10, time.Minute, logger, trustedProxies),
		RedeemLimiter:   ratelimit.New(rlStore, 20, time.Minute, logger, trustedProxies),
		Recordings:      recordingHandlers,
	})

	// Sample the active signaling room count into the Prometheus gauge.
	go sampleRooms(ctx, hub)

	// Periodically expire connection tokens that were minted but never redeemed
	// (the room-ended signal above only covers sessions that reached 'active';
	// a token nobody ever redeemed leaves its session stuck at 'pending').
	go sweepStalePendingSessions(ctx, sessionSvc, cfg.SessionTokenTTL, logger)

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

// sweepStalePendingSessions periodically expires 'pending' sessions whose
// connection token was minted but never redeemed. The cutoff is a multiple of
// the token TTL, not the TTL itself, so a token that's about to be redeemed
// right at its expiry boundary is never raced.
func sweepStalePendingSessions(ctx context.Context, svc *session.Service, tokenTTL time.Duration, logger *slog.Logger) {
	const cutoffMultiplier = 2
	const sweepInterval = 5 * time.Minute

	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweepCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			n, err := svc.ExpireStalePending(sweepCtx, cutoffMultiplier*tokenTTL)
			cancel()
			if err != nil {
				logger.Warn("sweep stale pending sessions failed", "err", err)
			} else if n > 0 {
				logger.Info("expired stale pending sessions", "count", n)
			}
		}
	}
}
