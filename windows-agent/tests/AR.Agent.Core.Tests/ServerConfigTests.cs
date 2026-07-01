using AR.Agent.Core;
using Xunit;

namespace AR.Agent.Core.Tests;

public class ServerConfigTests
{
    [Theory]
    [InlineData("http://localhost:8080", "ws://localhost:8080/")]
    [InlineData("https://support.example.com", "wss://support.example.com/")]
    [InlineData("https://support.example.com:8443/api", "wss://support.example.com:8443/api")]
    public void WebSocketBase_DerivesScheme(string http, string expectedWs)
    {
        var ws = ServerConfig.WebSocketBase(new Uri(http));
        Assert.Equal(expectedWs, ws.ToString());
    }

    [Fact]
    public void ResolveHttpBase_PrefersExplicitValue()
    {
        var uri = ServerConfig.ResolveHttpBase("https://explicit.example.com");
        Assert.Equal("https://explicit.example.com/", uri.ToString());
    }

    [Fact]
    public void ResolveHttpBase_FallsBackToDefault()
    {
        // With no explicit value and (typically) no env var set, use the default.
        var uri = ServerConfig.ResolveHttpBase(null);
        Assert.True(uri.IsAbsoluteUri);
    }
}
