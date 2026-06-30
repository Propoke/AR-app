package signaling

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// dialPeer connects a websocket client to the signaling test server for a room/role.
func dialPeer(t *testing.T, srv *httptest.Server, room, role string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/v1/signaling?room=" + room + "&role=" + role
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", role, err)
	}
	return conn
}

func readEnvelope(t *testing.T, conn *websocket.Conn) envelope {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("unmarshal %q: %v", data, err)
	}
	return env
}

func newTestServer() *httptest.Server {
	hub := NewHub()
	h := NewHandler(hub, slog.New(slog.NewTextHandler(io.Discard, nil)))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/signaling", h.ServeWS)
	return httptest.NewServer(mux)
}

// TestRelayForwardsBetweenPeers exercises the full handshake message flow: the
// second peer to join triggers peer-ready, and offer/answer/ice/annotation frames
// are forwarded to the counterpart with the sender's role stamped in `from`.
func TestRelayForwardsBetweenPeers(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()

	room := uuid.NewString()

	agent := dialPeer(t, srv, room, "agent")
	defer agent.Close()

	// The agent is alone, so it should not yet receive peer-ready.
	phone := dialPeer(t, srv, room, "phone")
	defer phone.Close()

	// The phone joined second, so it learns its counterpart is ready.
	if env := readEnvelope(t, phone); env.Type != "peer-ready" {
		t.Fatalf("phone expected peer-ready, got %q", env.Type)
	}

	// Agent sends an offer; phone should receive it stamped from=agent.
	if err := agent.WriteMessage(websocket.TextMessage,
		mustMarshal(envelope{Type: "offer", Payload: json.RawMessage(`{"sdp":"x"}`)}),
	); err != nil {
		t.Fatalf("agent write offer: %v", err)
	}
	env := readEnvelope(t, phone)
	if env.Type != "offer" || env.From != RoleAgent {
		t.Fatalf("phone expected offer from agent, got type=%q from=%q", env.Type, env.From)
	}

	// Phone answers; agent should receive it stamped from=phone.
	if err := phone.WriteMessage(websocket.TextMessage,
		mustMarshal(envelope{Type: "answer", Payload: json.RawMessage(`{"sdp":"y"}`)}),
	); err != nil {
		t.Fatalf("phone write answer: %v", err)
	}
	env = readEnvelope(t, agent)
	if env.Type != "answer" || env.From != RolePhone {
		t.Fatalf("agent expected answer from phone, got type=%q from=%q", env.Type, env.From)
	}

	// An annotation frame relays opaquely.
	if err := agent.WriteMessage(websocket.TextMessage,
		mustMarshal(envelope{Type: "annotation", Payload: json.RawMessage(`{"op":"create","id":"a1"}`)}),
	); err != nil {
		t.Fatalf("agent write annotation: %v", err)
	}
	env = readEnvelope(t, phone)
	if env.Type != "annotation" || env.From != RoleAgent {
		t.Fatalf("phone expected annotation from agent, got type=%q", env.Type)
	}
}

// TestRelayDeliversByeOnDisconnect verifies the surviving peer is told when its
// counterpart drops.
func TestRelayDeliversByeOnDisconnect(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()
	room := uuid.NewString()

	agent := dialPeer(t, srv, room, "agent")
	defer agent.Close()
	phone := dialPeer(t, srv, room, "phone")

	// Drain the peer-ready the phone receives.
	_ = readEnvelope(t, phone)

	// Phone leaves; agent should receive a bye stamped from=phone.
	phone.Close()
	env := readEnvelope(t, agent)
	if env.Type != "bye" || env.From != RolePhone {
		t.Fatalf("agent expected bye from phone, got type=%q from=%q", env.Type, env.From)
	}
}

// TestRelayRejectsBadParams ensures invalid room/role are refused at upgrade time.
func TestRelayRejectsBadParams(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()

	cases := map[string]string{
		"bad room": "/v1/signaling?room=not-a-uuid&role=agent",
		"bad role": "/v1/signaling?room=" + uuid.NewString() + "&role=bogus",
	}
	for name, path := range cases {
		t.Run(name, func(t *testing.T) {
			resp, err := http.Get(srv.URL + path)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d", resp.StatusCode)
			}
		})
	}
}
