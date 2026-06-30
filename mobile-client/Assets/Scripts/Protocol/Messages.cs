using System.Collections.Generic;
using Newtonsoft.Json;

namespace ARApp.Protocol
{
    /// <summary>Signaling envelope; mirrors shared/signaling.schema.json.</summary>
    public sealed class SignalingEnvelope
    {
        [JsonProperty("type")] public string Type;
        [JsonProperty("from", NullValueHandling = NullValueHandling.Ignore)] public string From;
        [JsonProperty("payload", NullValueHandling = NullValueHandling.Ignore)] public object Payload;

        public string Serialize() => JsonConvert.SerializeObject(this);
        public static SignalingEnvelope Parse(string json) =>
            JsonConvert.DeserializeObject<SignalingEnvelope>(json);
    }

    /// <summary>SDP body for offer/answer envelopes.</summary>
    public sealed class SdpPayload
    {
        [JsonProperty("sdp")] public string Sdp;
    }

    /// <summary>RTCIceCandidateInit wire shape.</summary>
    public sealed class IceCandidatePayload
    {
        [JsonProperty("candidate")] public string Candidate;
        [JsonProperty("sdpMid")] public string SdpMid;
        [JsonProperty("sdpMLineIndex")] public int? SdpMLineIndex;
    }

    public sealed class NormPoint
    {
        [JsonProperty("u")] public double U;
        [JsonProperty("v")] public double V;
    }

    /// <summary>AR annotation operation; mirrors shared/annotation.schema.json.</summary>
    public sealed class AnnotationEvent
    {
        [JsonProperty("op")] public string Op;
        [JsonProperty("id")] public string Id;
        [JsonProperty("kind", NullValueHandling = NullValueHandling.Ignore)] public string Kind;
        [JsonProperty("point", NullValueHandling = NullValueHandling.Ignore)] public NormPoint Point;
        [JsonProperty("path", NullValueHandling = NullValueHandling.Ignore)] public List<NormPoint> Path;
        [JsonProperty("frameId", NullValueHandling = NullValueHandling.Ignore)] public long? FrameId;
        [JsonProperty("text", NullValueHandling = NullValueHandling.Ignore)] public string Text;
        [JsonProperty("color", NullValueHandling = NullValueHandling.Ignore)] public string Color;
        [JsonProperty("anchorId", NullValueHandling = NullValueHandling.Ignore)] public string AnchorId;

        public string Serialize() => JsonConvert.SerializeObject(this);
        public static AnnotationEvent Parse(string json) =>
            JsonConvert.DeserializeObject<AnnotationEvent>(json);
    }

    /// <summary>Response from POST /v1/sessions/redeem.</summary>
    public sealed class JoinInfo
    {
        [JsonProperty("session_id")] public string SessionId;
        [JsonProperty("room")] public string Room;
        [JsonProperty("ice_servers")] public List<IceServerInfo> IceServers;
    }

    public sealed class IceServerInfo
    {
        [JsonProperty("urls")] public List<string> Urls;
        [JsonProperty("username")] public string Username;
        [JsonProperty("credential")] public string Credential;
    }
}
