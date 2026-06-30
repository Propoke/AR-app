package signaling

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 64 * 1024
	sendBuffer     = 32
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	// Origin checks are enforced at the reverse proxy / by the native clients,
	// which do not send a browser Origin header. Tighten if a web client is added.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// wsPeer is a WebSocket-backed Peer.
type wsPeer struct {
	id   string
	role Role
	conn *websocket.Conn
	send chan []byte
}

func (p *wsPeer) ID() string { return p.id }
func (p *wsPeer) Role() Role { return p.role }

// Send queues a frame, dropping it if the peer's buffer is full (slow consumer).
func (p *wsPeer) Send(frame []byte) bool {
	select {
	case p.send <- frame:
		return true
	default:
		return false
	}
}

// envelope is the signaling message wire format. Type is one of:
// "offer", "answer", "ice", "annotation", "bye". Payload is opaque to the relay.
type envelope struct {
	Type    string          `json:"type"`
	From    Role            `json:"from,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Authorizer validates that a token permits joining room as role. Returning a
// non-nil error rejects the connection. A nil Authorizer disables token checks
// (used by the DB-free dev relay and interop tests).
type Authorizer func(room, role, token string) error

// Handler upgrades a request to a WebSocket and relays signaling for a room.
type Handler struct {
	hub  *Hub
	log  *slog.Logger
	auth Authorizer
}

// NewHandler constructs a signaling Handler. Pass a nil Authorizer to disable
// token enforcement (development only).
func NewHandler(hub *Hub, log *slog.Logger, auth Authorizer) *Handler {
	return &Handler{hub: hub, log: log, auth: auth}
}

// ServeWS handles GET /v1/signaling?room=<id>&role=<agent|phone>.
//
// NOTE (Phase 1): the room id (a session UUID) acts as the bearer for the room.
// Phase 2 will require a short-lived signaling join token minted at redeem time.
func (h *Handler) ServeWS(w http.ResponseWriter, r *http.Request) {
	room := r.URL.Query().Get("room")
	if _, err := uuid.Parse(room); err != nil {
		http.Error(w, "invalid room", http.StatusBadRequest)
		return
	}
	role := Role(r.URL.Query().Get("role"))
	if role != RoleAgent && role != RolePhone {
		http.Error(w, "role must be agent or phone", http.StatusBadRequest)
		return
	}

	// Authorize with the per-session signaling token, if enforcement is enabled.
	if h.auth != nil {
		if err := h.auth(room, string(role), r.URL.Query().Get("token")); err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade already wrote an error response.
	}

	peer := &wsPeer{
		id:   uuid.NewString(),
		role: role,
		conn: conn,
		send: make(chan []byte, sendBuffer),
	}
	h.hub.Join(room, peer)
	h.log.Info("peer joined", "room", room, "role", role, "peer", peer.id)

	// Tell the newcomer whether its counterpart is already present, so the agent
	// knows when to send its WebRTC offer.
	if _, ok := h.hub.Counterpart(room, peer); ok {
		peer.Send(mustMarshal(envelope{Type: "peer-ready"}))
	}

	go h.writePump(peer)
	h.readPump(room, peer)
}

func (h *Handler) readPump(room string, peer *wsPeer) {
	defer func() {
		h.hub.Leave(room, peer)
		close(peer.send)
		_ = peer.conn.Close()
		// Notify the other side that this peer is gone.
		h.hub.Relay(room, peer, mustMarshal(envelope{Type: "bye", From: peer.role}))
		h.log.Info("peer left", "room", room, "role", peer.role, "peer", peer.id)
	}()

	peer.conn.SetReadLimit(maxMessageSize)
	_ = peer.conn.SetReadDeadline(time.Now().Add(pongWait))
	peer.conn.SetPongHandler(func(string) error {
		return peer.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		_, data, err := peer.conn.ReadMessage()
		if err != nil {
			return
		}
		var env envelope
		if err := json.Unmarshal(data, &env); err != nil {
			peer.Send(mustMarshal(envelope{Type: "error", Payload: json.RawMessage(`"malformed message"`)}))
			continue
		}
		env.From = peer.role
		h.hub.Relay(room, peer, mustMarshal(env))
	}
}

func (h *Handler) writePump(peer *wsPeer) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = peer.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-peer.send:
			_ = peer.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = peer.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := peer.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = peer.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := peer.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func mustMarshal(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
