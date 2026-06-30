using System.Text.Json;
using System.Text.Json.Serialization;

namespace AR.Agent.Core.Protocol;

/// <summary>A normalized point on the agent's video frame (0..1 in each axis).</summary>
public sealed class NormPoint
{
    [JsonPropertyName("u")] public double U { get; set; }
    [JsonPropertyName("v")] public double V { get; set; }
}

/// <summary>
/// An AR annotation operation authored by the technician and rendered as a
/// world-anchored marker on the phone. Mirrors shared/annotation.schema.json.
/// </summary>
public sealed class AnnotationEvent
{
    /// <summary>"create" | "update" | "delete" | "clear".</summary>
    [JsonPropertyName("op")] public string Op { get; set; } = "";

    /// <summary>Client-generated id correlating the agent annotation with the phone anchor.</summary>
    [JsonPropertyName("id")] public string Id { get; set; } = "";

    /// <summary>"arrow" | "circle" | "text" | "marker" | "freehand".</summary>
    [JsonPropertyName("kind")]
    [JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)]
    public string? Kind { get; set; }

    /// <summary>Normalized tap point the phone raycasts into the AR scene.</summary>
    [JsonPropertyName("point")]
    [JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)]
    public NormPoint? Point { get; set; }

    /// <summary>Ordered points for a freehand stroke.</summary>
    [JsonPropertyName("path")]
    [JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)]
    public List<NormPoint>? Path { get; set; }

    /// <summary>Video frame the point refers to, for timestamp matching.</summary>
    [JsonPropertyName("frameId")]
    [JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)]
    public long? FrameId { get; set; }

    /// <summary>Label text for kind=text.</summary>
    [JsonPropertyName("text")]
    [JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)]
    public string? Text { get; set; }

    /// <summary>Marker color as #RRGGBB.</summary>
    [JsonPropertyName("color")]
    [JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)]
    public string? Color { get; set; }

    /// <summary>Set by the phone in echoed events: the AR anchor id backing this annotation.</summary>
    [JsonPropertyName("anchorId")]
    [JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)]
    public string? AnchorId { get; set; }

    public string Serialize() => JsonSerializer.Serialize(this, SignalingEnvelope.JsonOptions);

    public static AnnotationEvent Parse(string json) =>
        JsonSerializer.Deserialize<AnnotationEvent>(json, SignalingEnvelope.JsonOptions)
        ?? throw new JsonException("empty annotation event");
}
