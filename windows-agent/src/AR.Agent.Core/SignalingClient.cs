using System.Net.WebSockets;
using System.Text;
using AR.Agent.Core.Protocol;

namespace AR.Agent.Core;

/// <summary>
/// Connects to the signaling WebSocket as the "agent" peer and relays envelopes
/// to/from the counterpart. The transport is the framework-agnostic core shared
/// with the WinUI app; it raises <see cref="EnvelopeReceived"/> on each message.
/// </summary>
public sealed class SignalingClient : IAsyncDisposable
{
    private readonly Uri _baseWsUrl;
    private readonly ClientWebSocket _ws = new();
    private readonly SemaphoreSlim _sendLock = new(1, 1);

    /// <summary>Raised for every envelope received from the relay.</summary>
    public event Action<SignalingEnvelope>? EnvelopeReceived;

    /// <param name="baseWsUrl">e.g. wss://api.example.com (no path).</param>
    public SignalingClient(Uri baseWsUrl) => _baseWsUrl = baseWsUrl;

    /// <summary>
    /// Opens the WebSocket for the given room as the agent role, presenting the
    /// per-session signaling join token via the X-Signaling-Token request
    /// header (rather than a query parameter) so it isn't captured verbatim in
    /// reverse-proxy or load-balancer access logs, which typically record the
    /// full request URL including its query string but not arbitrary headers.
    /// </summary>
    public async Task ConnectAsync(string room, string signalingToken, CancellationToken ct = default)
    {
        var url = new Uri(_baseWsUrl, $"/v1/signaling?room={Uri.EscapeDataString(room)}&role=agent");
        _ws.Options.SetRequestHeader("X-Signaling-Token", signalingToken);
        await _ws.ConnectAsync(url, ct).ConfigureAwait(false);
    }

    /// <summary>Sends an envelope to the relay (forwarded to the phone).</summary>
    public async Task SendAsync(SignalingEnvelope env, CancellationToken ct = default)
    {
        var bytes = Encoding.UTF8.GetBytes(env.Serialize());
        await _sendLock.WaitAsync(ct).ConfigureAwait(false);
        try
        {
            await _ws.SendAsync(bytes, WebSocketMessageType.Text, endOfMessage: true, ct)
                .ConfigureAwait(false);
        }
        finally
        {
            _sendLock.Release();
        }
    }

    /// <summary>
    /// Reads frames until the socket closes or the token is cancelled, raising
    /// <see cref="EnvelopeReceived"/> for each. Call once after connecting.
    /// </summary>
    public async Task ReceiveLoopAsync(CancellationToken ct = default)
    {
        var buffer = new byte[64 * 1024];
        var sb = new StringBuilder();
        while (_ws.State == WebSocketState.Open && !ct.IsCancellationRequested)
        {
            WebSocketReceiveResult result;
            try
            {
                result = await _ws.ReceiveAsync(buffer, ct).ConfigureAwait(false);
            }
            catch (OperationCanceledException)
            {
                break;
            }

            if (result.MessageType == WebSocketMessageType.Close)
            {
                await _ws.CloseAsync(WebSocketCloseStatus.NormalClosure, "bye", CancellationToken.None)
                    .ConfigureAwait(false);
                break;
            }

            sb.Append(Encoding.UTF8.GetString(buffer, 0, result.Count));
            if (!result.EndOfMessage)
                continue;

            var json = sb.ToString();
            sb.Clear();
            try
            {
                EnvelopeReceived?.Invoke(SignalingEnvelope.Parse(json));
            }
            catch
            {
                // Ignore malformed frames; the relay or peer should not send them.
            }
        }
    }

    public async ValueTask DisposeAsync()
    {
        try
        {
            if (_ws.State == WebSocketState.Open)
                await _ws.CloseAsync(WebSocketCloseStatus.NormalClosure, "dispose", CancellationToken.None)
                    .ConfigureAwait(false);
        }
        catch
        {
            // best-effort close
        }
        _ws.Dispose();
        _sendLock.Dispose();
    }
}
