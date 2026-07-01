using System;
using System.Net.WebSockets;
using System.Text;
using System.Threading;
using System.Threading.Tasks;
using ARApp.Protocol;
using UnityEngine;

namespace ARApp.Signaling
{
    /// <summary>
    /// Connects to the backend signaling WebSocket as the "phone" peer. Runs on a
    /// background task; received envelopes are marshalled to the main thread by the
    /// caller (see SessionController) before touching Unity objects.
    /// </summary>
    public sealed class SignalingClient : IDisposable
    {
        private readonly Uri _baseWsUrl;
        private readonly ClientWebSocket _ws = new ClientWebSocket();
        private readonly CancellationTokenSource _cts = new CancellationTokenSource();

        /// <summary>Raised on the background thread for each received envelope.</summary>
        public event Action<SignalingEnvelope> EnvelopeReceived;

        public SignalingClient(Uri baseWsUrl) => _baseWsUrl = baseWsUrl;

        // The signaling token is sent via the X-Signaling-Token request header
        // rather than a query parameter, so it isn't captured verbatim in
        // reverse-proxy/load-balancer access logs (which typically record the
        // full request URL but not arbitrary headers). The backend still also
        // accepts a "token" query parameter for compatibility, in case a given
        // platform's WebSocket stack can't set custom headers before the
        // handshake — see docs/handshake-troubleshooting.md if connections that
        // worked with the old query-string form start failing after an update.
        public async Task ConnectAsync(string room, string signalingToken)
        {
            var url = new Uri(_baseWsUrl, $"/v1/signaling?room={Uri.EscapeDataString(room)}&role=phone");
            _ws.Options.SetRequestHeader("X-Signaling-Token", signalingToken);
            await _ws.ConnectAsync(url, _cts.Token);
            _ = Task.Run(ReceiveLoop);
        }

        public async Task SendAsync(string type, object payload)
        {
            var env = new SignalingEnvelope { Type = type, Payload = payload };
            var bytes = Encoding.UTF8.GetBytes(env.Serialize());
            await _ws.SendAsync(new ArraySegment<byte>(bytes), WebSocketMessageType.Text, true, _cts.Token);
        }

        private async Task ReceiveLoop()
        {
            var buffer = new byte[64 * 1024];
            var sb = new StringBuilder();
            try
            {
                while (_ws.State == WebSocketState.Open && !_cts.IsCancellationRequested)
                {
                    var result = await _ws.ReceiveAsync(new ArraySegment<byte>(buffer), _cts.Token);
                    if (result.MessageType == WebSocketMessageType.Close)
                        break;

                    sb.Append(Encoding.UTF8.GetString(buffer, 0, result.Count));
                    if (!result.EndOfMessage)
                        continue;

                    var json = sb.ToString();
                    sb.Clear();
                    try
                    {
                        EnvelopeReceived?.Invoke(SignalingEnvelope.Parse(json));
                    }
                    catch (Exception ex)
                    {
                        Debug.LogWarning($"[signaling] bad frame: {ex.Message}");
                    }
                }
            }
            catch (OperationCanceledException)
            {
                // shutting down
            }
            catch (Exception ex)
            {
                Debug.LogError($"[signaling] receive error: {ex.Message}");
            }
        }

        public void Dispose()
        {
            _cts.Cancel();
            try { _ws.Dispose(); } catch { /* best effort */ }
            _cts.Dispose();
        }
    }
}
