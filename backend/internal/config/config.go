// Package config loads runtime configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all runtime configuration for the backend.
type Config struct {
	HTTPAddr string

	DatabaseURL string
	RedisURL    string

	// JWTSecret signs access and refresh tokens. Must be set in production.
	JWTSecret       []byte
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration

	// SessionTokenTTL is how long a connection token (ID + PIN) stays valid.
	SessionTokenTTL time.Duration

	// SignalingTokenTTL bounds a signaling join token's lifetime (a session's max length).
	SignalingTokenTTL time.Duration

	// TURNSecret is the shared secret used to mint coturn TURN REST credentials.
	TURNSecret  string
	TURNRealm   string
	TURNURLs    []string
	TURNCredTTL time.Duration

	// Object storage for session recordings. Recordings are enabled only when
	// S3Endpoint is set.
	S3Endpoint      string
	S3AccessKey     string
	S3SecretKey     string
	S3Bucket        string
	S3Region        string
	S3UseSSL        bool
	RecordingURLTTL time.Duration
}

// RecordingsEnabled reports whether object storage is configured.
func (c *Config) RecordingsEnabled() bool { return c.S3Endpoint != "" }

// Load reads configuration from the environment, applying sensible defaults for
// local development. It returns an error only for values that cannot be parsed.
func Load() (*Config, error) {
	c := &Config{
		HTTPAddr:    getenv("HTTP_ADDR", ":8080"),
		DatabaseURL: getenv("DATABASE_URL", "postgres://ar:ar@localhost:5432/ar?sslmode=disable"),
		RedisURL:    getenv("REDIS_URL", "redis://localhost:6379/0"),
		JWTSecret:   []byte(getenv("JWT_SECRET", "dev-insecure-secret-change-me")),
		TURNSecret:  getenv("TURN_SECRET", "dev-turn-secret"),
		TURNRealm:   getenv("TURN_REALM", "ar.local"),
		TURNURLs:    splitNonEmpty(getenv("TURN_URLS", "turn:localhost:3478?transport=udp")),
		S3Endpoint:  getenv("S3_ENDPOINT", ""),
		S3AccessKey: getenv("S3_ACCESS_KEY", ""),
		S3SecretKey: getenv("S3_SECRET_KEY", ""),
		S3Bucket:    getenv("S3_BUCKET", "recordings"),
		S3Region:    getenv("S3_REGION", "us-east-1"),
		S3UseSSL:    getenv("S3_USE_SSL", "true") == "true",
	}

	var err error
	if c.AccessTokenTTL, err = getdur("ACCESS_TOKEN_TTL", 15*time.Minute); err != nil {
		return nil, err
	}
	if c.RefreshTokenTTL, err = getdur("REFRESH_TOKEN_TTL", 720*time.Hour); err != nil {
		return nil, err
	}
	if c.SessionTokenTTL, err = getdur("SESSION_TOKEN_TTL", 10*time.Minute); err != nil {
		return nil, err
	}
	if c.SignalingTokenTTL, err = getdur("SIGNALING_TOKEN_TTL", 4*time.Hour); err != nil {
		return nil, err
	}
	if c.TURNCredTTL, err = getdur("TURN_CRED_TTL", 1*time.Hour); err != nil {
		return nil, err
	}
	if c.RecordingURLTTL, err = getdur("RECORDING_URL_TTL", 15*time.Minute); err != nil {
		return nil, err
	}
	return c, nil
}

func getenv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func getdur(key string, def time.Duration) (time.Duration, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def, nil
	}
	// Allow either a Go duration ("15m") or a plain number of seconds.
	if d, err := time.ParseDuration(v); err == nil {
		return d, nil
	}
	secs, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("config %s: invalid duration %q", key, v)
	}
	return time.Duration(secs) * time.Second, nil
}

func splitNonEmpty(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == ',' {
			if cur != "" {
				out = append(out, cur)
			}
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
