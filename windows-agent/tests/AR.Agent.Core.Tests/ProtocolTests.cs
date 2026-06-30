using System.Text.Json;
using AR.Agent.Core.Protocol;
using Xunit;

namespace AR.Agent.Core.Tests;

public class ProtocolTests
{
    [Fact]
    public void AnnotationEvent_RoundTrips()
    {
        var evt = new AnnotationEvent
        {
            Op = "create",
            Id = "abc",
            Kind = "arrow",
            Point = new NormPoint { U = 0.5, V = 0.42 },
            Color = "#ff3b30",
        };

        var json = evt.Serialize();
        var back = AnnotationEvent.Parse(json);

        Assert.Equal("create", back.Op);
        Assert.Equal("arrow", back.Kind);
        Assert.NotNull(back.Point);
        Assert.Equal(0.5, back.Point!.U, 3);
        Assert.Equal("#ff3b30", back.Color);
    }

    [Fact]
    public void AnnotationEvent_OmitsNullFields()
    {
        var evt = new AnnotationEvent { Op = "clear", Id = "x" };
        var json = evt.Serialize();

        // Optional fields must not appear when null, matching the shared schema.
        Assert.DoesNotContain("\"kind\"", json);
        Assert.DoesNotContain("\"point\"", json);
        Assert.DoesNotContain("\"color\"", json);
        Assert.Contains("\"op\":\"clear\"", json);
    }

    [Fact]
    public void SignalingEnvelope_ParsesPayloadAsElement()
    {
        const string json = """{"type":"offer","from":"agent","payload":{"sdp":"v=0..."}}""";
        var env = SignalingEnvelope.Parse(json);

        Assert.Equal("offer", env.Type);
        Assert.Equal("agent", env.From);

        var sdp = env.Payload.Deserialize<SdpPayload>(SignalingEnvelope.JsonOptions);
        Assert.NotNull(sdp);
        Assert.Equal("v=0...", sdp!.Sdp);
    }

    [Fact]
    public void SignalingEnvelope_SerializesPayloadFromElement()
    {
        var env = new SignalingEnvelope
        {
            Type = "answer",
            Payload = JsonSerializer.SerializeToElement(new SdpPayload { Sdp = "abc" }),
        };

        var json = env.Serialize();
        Assert.Contains("\"type\":\"answer\"", json);
        Assert.Contains("\"sdp\":\"abc\"", json);
        // `from` is null here and must be omitted.
        Assert.DoesNotContain("\"from\"", json);
    }
}
