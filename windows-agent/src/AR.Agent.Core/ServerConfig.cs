namespace AR.Agent.Core;

/// <summary>
/// Resolves the backend endpoints the agent talks to. The signaling WebSocket
/// lives on the same host as the HTTP API, so its base URL is derived from the
/// HTTP base (http→ws, https→wss) — callers only configure one URL.
/// </summary>
public static class ServerConfig
{
    /// <summary>
    /// Environment variable that overrides the backend URL (useful for testing and
    /// deployment without recompiling).
    /// </summary>
    public const string EnvVar = "AR_BACKEND_URL";

    /// <summary>The default backend URL for local development.</summary>
    public const string DefaultHttp = "http://localhost:8080";

    /// <summary>
    /// Resolves the HTTP base URL: an explicit value wins, then the environment
    /// variable, then the local-dev default.
    /// </summary>
    public static Uri ResolveHttpBase(string? explicitUrl = null)
    {
        var raw = !string.IsNullOrWhiteSpace(explicitUrl)
            ? explicitUrl!
            : Environment.GetEnvironmentVariable(EnvVar);
        if (string.IsNullOrWhiteSpace(raw))
            raw = DefaultHttp;
        return new Uri(raw.Trim(), UriKind.Absolute);
    }

    /// <summary>
    /// Derives the WebSocket base URL from an HTTP(S) base URL, preserving host,
    /// port, and any path prefix.
    /// </summary>
    public static Uri WebSocketBase(Uri httpBase)
    {
        var scheme = httpBase.Scheme switch
        {
            "https" => "wss",
            "http" => "ws",
            "wss" or "ws" => httpBase.Scheme,
            _ => throw new ArgumentException($"unsupported scheme: {httpBase.Scheme}"),
        };
        var builder = new UriBuilder(httpBase) { Scheme = scheme };
        // UriBuilder forces a default port; clear it so the original is preserved.
        if (httpBase.IsDefaultPort)
            builder.Port = -1;
        return builder.Uri;
    }
}
