using System.Net;
using System.Net.Http;
using System.Text;
using System.Threading;
using System.Threading.Tasks;
using AR.Agent.Core;
using Xunit;

namespace AR.Agent.Core.Tests;

public class BackendClientTests
{
    // Routes requests to a canned responder and records the last request seen.
    private sealed class StubHandler : HttpMessageHandler
    {
        private readonly System.Func<HttpRequestMessage, HttpResponseMessage> _responder;
        public HttpRequestMessage? LastRequest { get; private set; }

        public StubHandler(System.Func<HttpRequestMessage, HttpResponseMessage> responder) => _responder = responder;

        protected override Task<HttpResponseMessage> SendAsync(HttpRequestMessage request, CancellationToken ct)
        {
            LastRequest = request;
            return Task.FromResult(_responder(request));
        }
    }

    private static HttpResponseMessage Json(HttpStatusCode code, string body) => new(code)
    {
        Content = new StringContent(body, Encoding.UTF8, "application/json"),
    };

    private static BackendClient NewClient(StubHandler handler) =>
        new(new System.Uri("http://backend.test"), new HttpClient(handler));

    [Fact]
    public async Task CheckHealth_ReturnsTrueOn200_FalseOnError()
    {
        var okClient = NewClient(new StubHandler(_ => new HttpResponseMessage(HttpStatusCode.OK)));
        Assert.True(await okClient.CheckHealthAsync());

        var downClient = NewClient(new StubHandler(_ => new HttpResponseMessage(HttpStatusCode.ServiceUnavailable)));
        Assert.False(await downClient.CheckHealthAsync());
    }

    [Fact]
    public async Task Login_StoresAccessToken()
    {
        var handler = new StubHandler(_ => Json(HttpStatusCode.OK,
            """{"access_token":"acc","refresh_token":"ref"}"""));
        var client = NewClient(handler);

        await client.LoginAsync("tech@acme.com", "hunter2hunter2");

        Assert.True(client.IsAuthenticated);
        Assert.Equal("/v1/auth/login", handler.LastRequest!.RequestUri!.AbsolutePath);
        Assert.Equal(HttpMethod.Post, handler.LastRequest.Method);
    }

    [Fact]
    public async Task MintSession_SendsBearerAndParsesResponse()
    {
        var handler = new StubHandler(req =>
            req.RequestUri!.AbsolutePath == "/v1/auth/login"
                ? Json(HttpStatusCode.OK, """{"access_token":"acc","refresh_token":"ref"}""")
                : Json(HttpStatusCode.Created, """
                    {"session_id":"s1","connect_id":"048213765","pin":"509134",
                     "room":"s1","signaling_token":"sig",
                     "ice_servers":[{"urls":["turn:x"],"username":"u","credential":"c"}]}
                    """));
        var client = NewClient(handler);
        await client.LoginAsync("tech@acme.com", "hunter2hunter2");

        var minted = await client.MintSessionAsync();

        Assert.Equal("048213765", minted.ConnectId);
        Assert.Equal("509134", minted.Pin);
        Assert.Equal("sig", minted.SignalingToken);
        Assert.Single(minted.IceServers);
        // The mint request carried the bearer token from login.
        Assert.Equal("Bearer", handler.LastRequest!.Headers.Authorization!.Scheme);
        Assert.Equal("acc", handler.LastRequest.Headers.Authorization.Parameter);
    }

    [Fact]
    public async Task MintSession_WithoutLogin_Throws()
    {
        var client = NewClient(new StubHandler(_ => new HttpResponseMessage(HttpStatusCode.OK)));
        await Assert.ThrowsAsync<System.InvalidOperationException>(() => client.MintSessionAsync());
    }
}
