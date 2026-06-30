// Package turn mints ephemeral coturn credentials using the TURN REST API
// shared-secret scheme (coturn `use-auth-secret`).
package turn

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"time"
)

// ICEServer is a WebRTC ICE server description returned to clients.
type ICEServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

// Minter generates time-limited TURN credentials.
type Minter struct {
	secret string
	urls   []string
	ttl    time.Duration
}

// NewMinter constructs a TURN credential minter.
func NewMinter(secret string, urls []string, ttl time.Duration) *Minter {
	return &Minter{secret: secret, urls: urls, ttl: ttl}
}

// Credentials returns a STUN/TURN ICE server list with a username/credential pair
// valid for the configured TTL. The username embeds the expiry as a unix
// timestamp, per the coturn REST scheme; the credential is the HMAC-SHA1 of the
// username keyed by the shared secret.
func (m *Minter) Credentials(identifier string) []ICEServer {
	if m.secret == "" || len(m.urls) == 0 {
		return nil
	}
	expiry := time.Now().Add(m.ttl).Unix()
	username := fmt.Sprintf("%d:%s", expiry, identifier)

	mac := hmac.New(sha1.New, []byte(m.secret))
	mac.Write([]byte(username))
	credential := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	return []ICEServer{{
		URLs:       m.urls,
		Username:   username,
		Credential: credential,
	}}
}
