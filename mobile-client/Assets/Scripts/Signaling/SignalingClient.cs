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

        public async Task ConnectAsync(string room)
        {
            var url = new Uri(_baseWsUrl, $"/v1/signaling?room={Uri.EscapeDataString(room)}&role=phone");
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
