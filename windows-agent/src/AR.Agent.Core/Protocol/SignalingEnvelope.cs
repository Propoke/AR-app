using System.Text.Json;
using System.Text.Json.Serialization;

namespace AR.Agent.Core.Protocol;

/// <summary>
/// Wire format for messages over the signaling WebSocket (/v1/signaling).
/// Mirrors shared/signaling.schema.json.
/// </summary>
public sealed class SignalingEnvelope
{
    [JsonPropertyName("type")]
    public string Type { get; set; } = "";

    /// <summary>Set by the relay to the sender's role ("agent" or "phone").</summary>
    [JsonPropertyName("from")]
    public string? From { get; set; }

    /// <summary>Type-specific body, left as raw JSON for the caller to interpret.</summary>
    [JsonPropertyName("payload")]
    public JsonElement Payload { get; set; }

    public static readonly JsonSerializerOptions JsonOptions = new()
    {
        DefaultIgnoreCondition = JsonIgnoreCondition.WhenWritingNull,
    };

    public string Serialize() => JsonSerializer.Serialize(this, JsonOptions);

    public static SignalingEnvelope Parse(string json) =>
        JsonSerializer.Deserialize<SignalingEnvelope>(json, JsonOptions)
        ?? throw new JsonException("empty signaling envelope");
}

/// <summary>SDP body for "offer"/"answer" envelopes: { "sdp": "..." }.</summary>
public sealed class SdpPayload
{
    [JsonPropertyName("sdp")]
    public string Sdp { get; set; } = "";
}
