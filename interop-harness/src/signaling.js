// Minimal signaling client for the harness. Mirrors the contract that the .NET
// agent and Unity client implement: connect to /v1/signaling?room&role and
// exchange envelopes of { type, from, payload }.
import { WebSocket } from "ws";

export class SignalingClient {
  /**
   * @param {string} baseURL e.g. ws://127.0.0.1:8081
   * @param {string} room    session id (uuid)
   * @param {"agent"|"phone"} role
   */
  constructor(baseURL, room, role) {
    this.url = `${baseURL}/v1/signaling?room=${room}&role=${role}`;
    this.role = role;
    this.handlers = new Map();
    this.ws = null;
  }

  connect() {
    return new Promise((resolve, reject) => {
      this.ws = new WebSocket(this.url);
      this.ws.on("open", () => resolve());
      this.ws.on("error", reject);
      this.ws.on("message", (data) => {
        let env;
        try {
          env = JSON.parse(data.toString());
        } catch {
          return;
        }
        const handler = this.handlers.get(env.type);
        if (handler) handler(env);
      });
    });
  }

  /** Register a handler for an envelope type ("peer-ready", "offer", "answer", "ice", ...). */
  on(type, handler) {
    this.handlers.set(type, handler);
  }

  send(type, payload) {
    this.ws.send(JSON.stringify({ type, payload }));
  }

  close() {
    if (this.ws) this.ws.close();
  }
}
