using System.Net.Http.Json;
using System.Text.Json.Serialization;

namespace AR.Agent.Core;

/// <summary>The ICE server list returned by the backend (mirrors turn.ICEServer).</summary>
public sealed class IceServer
{
    [JsonPropertyName("urls")] public List<string> Urls { get; set; } = new();
    [JsonPropertyName("username")] public string? Username { get; set; }
    [JsonPropertyName("credential")] public string? Credential { get; set; }
}

/// <summary>Result of minting a connection token (POST /v1/sessions).</summary>
public sealed class MintedSession
{
    [JsonPropertyName("session_id")] public string SessionId { get; set; } = "";
    [JsonPropertyName("connect_id")] public string ConnectId { get; set; } = "";
    [JsonPropertyName("pin")] public string Pin { get; set; } = "";
    [JsonPropertyName("room")] public string Room { get; set; } = "";
    [JsonPropertyName("expires_at")] public DateTimeOffset ExpiresAt { get; set; }
    [JsonPropertyName("ice_servers")] public List<IceServer> IceServers { get; set; } = new();
}

internal sealed class TokenPair
{
    [JsonPropertyName("access_token")] public string AccessToken { get; set; } = "";
    [JsonPropertyName("refresh_token")] public string RefreshToken { get; set; } = "";
}

/// <summary>
/// HTTP client for the AR backend: authenticates the technician and mints
/// connection tokens. Holds the access token for authenticated calls.
/// </summary>
public sealed class BackendClient
{
    private readonly HttpClient _http;
    private string? _accessToken;

    public BackendClient(Uri baseUrl, HttpClient? http = null)
    {
        _http = http ?? new HttpClient();
        _http.BaseAddress = baseUrl;
    }

    public bool IsAuthenticated => _accessToken is not null;

    /// <summary>Logs in and stores the access token for subsequent calls.</summary>
    public async Task LoginAsync(string email, string password, CancellationToken ct = default)
    {
        var resp = await _http.PostAsJsonAsync("/v1/auth/login",
            new { email, password }, ct).ConfigureAwait(false);
        resp.EnsureSuccessStatusCode();
        var tokens = await resp.Content.ReadFromJsonAsync<TokenPair>(cancellationToken: ct)
            .ConfigureAwait(false) ?? throw new InvalidOperationException("empty login response");
        _accessToken = tokens.AccessToken;
    }

    /// <summary>Mints a new connection token for an end-user to redeem.</summary>
    public async Task<MintedSession> MintSessionAsync(CancellationToken ct = default)
    {
        using var req = new HttpRequestMessage(HttpMethod.Post, "/v1/sessions");
        req.Headers.Authorization = AuthHeader();
        var resp = await _http.SendAsync(req, ct).ConfigureAwait(false);
        resp.EnsureSuccessStatusCode();
        return await resp.Content.ReadFromJsonAsync<MintedSession>(cancellationToken: ct)
            .ConfigureAwait(false) ?? throw new InvalidOperationException("empty mint response");
    }

    private System.Net.Http.Headers.AuthenticationHeaderValue AuthHeader()
    {
        if (_accessToken is null)
            throw new InvalidOperationException("not authenticated; call LoginAsync first");
        return new System.Net.Http.Headers.AuthenticationHeaderValue("Bearer", _accessToken);
    }
}
