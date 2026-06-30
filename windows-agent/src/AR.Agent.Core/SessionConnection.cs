using System.Net;
using System.Text;
using System.Text.Json;
using AR.Agent.Core.Media;
using AR.Agent.Core.Protocol;
using SIPSorcery.Net;
using SIPSorceryMedia.Abstractions;

namespace AR.Agent.Core;

/// <summary>
/// Owns the agent side of a WebRTC session: it builds the peer connection (VP8
/// video receive, two-way Opus audio, and the "annotations" data channel), drives
/// the offer/ICE handshake through a <see cref="SignalingClient"/>, and surfaces
/// incoming annotations and connection-state changes.
///
/// Codec lock: the agent offers only VP8 + Opus so it interoperates with the
/// Unity client's libwebrtc stack (see shared/README.md).
/// </summary>
public sealed class SessionConnection : IAsyncDisposable
{
    private const string AnnotationChannel = "annotations";

    private readonly SignalingClient _signaling;
    private readonly RTCPeerConnection _pc;
    private RTCDataChannel? _annotations;

    /// <summary>Raised when the phone sends (or echoes) an annotation event.</summary>
    public event Action<AnnotationEvent>? AnnotationReceived;

    /// <summary>Raised on every peer-connection state change.</summary>
    public event Action<RTCPeerConnectionState>? ConnectionStateChanged;

    private IMicrophone? _microphone;
    private ISpeaker? _speaker;
    private IVideoDecoder? _videoDecoder;
    private IVideoRenderer? _videoRenderer;

    public SessionConnection(SignalingClient signaling, IReadOnlyList<IceServer> iceServers)
    {
        _signaling = signaling;

        var config = new RTCConfiguration
        {
            iceServers = iceServers.Select(s => new RTCIceServer
            {
                urls = string.Join(',', s.Urls),
                username = s.Username,
                credential = s.Credential,
            }).ToList(),
        };
        _pc = new RTCPeerConnection(config);

        // Receive the phone's camera (VP8); send and receive audio (Opus).
        var videoTrack = new MediaStreamTrack(
            new List<VideoFormat> { new(VideoCodecsEnum.VP8, 96) },
            MediaStreamStatusEnum.RecvOnly);
        _pc.addTrack(videoTrack);

        var audioTrack = new MediaStreamTrack(
            new List<AudioFormat> { new(AudioCodecsEnum.OPUS, 111, 48000, 2) },
            MediaStreamStatusEnum.SendRecv);
        _pc.addTrack(audioTrack);

        _pc.onicecandidate += OnLocalIceCandidate;
        _pc.onconnectionstatechange += state => ConnectionStateChanged?.Invoke(state);

        // The phone may also open a data channel; accept whichever side creates it.
        _pc.ondatachannel += channel =>
        {
            if (channel.label == AnnotationChannel)
                BindAnnotationChannel(channel);
        };

        _signaling.EnvelopeReceived += OnSignalingEnvelope;
    }

    /// <summary>
    /// Wires audio and video devices to the session: the microphone's encoded
    /// samples are sent to the phone, received audio is played on the speaker, and
    /// received video frames are decoded and rendered. Call before StartAsync.
    /// </summary>
    public void AttachMedia(IMicrophone microphone, ISpeaker speaker, IVideoDecoder videoDecoder, IVideoRenderer videoRenderer)
    {
        _microphone = microphone;
        _speaker = speaker;
        _videoDecoder = videoDecoder;
        _videoRenderer = videoRenderer;

        // Outgoing audio: the microphone produces encoded Opus samples we forward.
        _microphone.EncodedSampleReady += (durationRtpUnits, sample) =>
        {
            try { _pc.SendAudio(durationRtpUnits, sample); }
            catch { /* connection may be tearing down */ }
        };

        // Incoming media: decode video frames to render, and play received audio.
        _pc.OnVideoFrameReceived += OnVideoFrame;
        _pc.OnRtpPacketReceived += OnRtpPacket;

        _microphone.Start();
    }

    private void OnVideoFrame(IPEndPoint _, uint __, byte[] encodedSample, VideoFormat ___)
    {
        var frame = _videoDecoder?.Decode(encodedSample);
        if (frame is { } f)
            _videoRenderer?.Render(f);
    }

    private void OnRtpPacket(IPEndPoint _, SDPMediaTypesEnum mediaType, RTPPacket packet)
    {
        if (mediaType == SDPMediaTypesEnum.audio)
            _speaker?.PlayEncoded(packet.Payload);
    }

    /// <summary>
    /// Creates the annotations data channel and sends the SDP offer. Call once the
    /// relay reports the phone is present (a "peer-ready" envelope).
    /// </summary>
    public async Task StartAsync(CancellationToken ct = default)
    {
        var channel = await _pc.createDataChannel(AnnotationChannel).ConfigureAwait(false);
        BindAnnotationChannel(channel);

        var offer = _pc.createOffer(null);
        await _pc.setLocalDescription(offer).ConfigureAwait(false);

        await _signaling.SendAsync(new SignalingEnvelope
        {
            Type = "offer",
            Payload = ToElement(new SdpPayload { Sdp = offer.sdp }),
        }, ct).ConfigureAwait(false);
    }

    /// <summary>Sends an annotation to the phone over the data channel.</summary>
    public void SendAnnotation(AnnotationEvent annotation)
    {
        if (_annotations is null || _annotations.readyState != RTCDataChannelState.open)
            throw new InvalidOperationException("annotation channel is not open");
        _annotations.send(annotation.Serialize());
    }

    private void OnSignalingEnvelope(SignalingEnvelope env)
    {
        switch (env.Type)
        {
            case "peer-ready":
                // The app decides when to StartAsync; nothing to do at the transport level.
                break;

            case "answer":
                var sdp = env.Payload.Deserialize<SdpPayload>(SignalingEnvelope.JsonOptions);
                if (sdp is not null)
                {
                    _pc.setRemoteDescription(new RTCSessionDescriptionInit
                    {
                        type = RTCSdpType.answer,
                        sdp = sdp.Sdp,
                    });
                }
                break;

            case "ice":
                var cand = env.Payload.Deserialize<IceCandidateDto>(SignalingEnvelope.JsonOptions);
                if (cand?.Candidate is not null)
                {
                    _pc.addIceCandidate(new RTCIceCandidateInit
                    {
                        candidate = cand.Candidate,
                        sdpMid = cand.SdpMid,
                        sdpMLineIndex = cand.SdpMLineIndex ?? 0,
                    });
                }
                break;

            case "bye":
                ConnectionStateChanged?.Invoke(RTCPeerConnectionState.closed);
                break;
        }
    }

    private async void OnLocalIceCandidate(RTCIceCandidate candidate)
    {
        if (candidate is null)
            return;
        try
        {
            await _signaling.SendAsync(new SignalingEnvelope
            {
                Type = "ice",
                Payload = ToElement(new IceCandidateDto
                {
                    Candidate = candidate.candidate,
                    SdpMid = candidate.sdpMid,
                    SdpMLineIndex = candidate.sdpMLineIndex,
                }),
            }).ConfigureAwait(false);
        }
        catch
        {
            // A dropped candidate is non-fatal; ICE will continue with the rest.
        }
    }

    private void BindAnnotationChannel(RTCDataChannel channel)
    {
        _annotations = channel;
        channel.onmessage += (_, _, data) =>
        {
            try
            {
                AnnotationReceived?.Invoke(AnnotationEvent.Parse(Encoding.UTF8.GetString(data)));
            }
            catch
            {
                // Ignore malformed annotation frames.
            }
        };
    }

    private static JsonElement ToElement(object value) =>
        JsonSerializer.SerializeToElement(value, SignalingEnvelope.JsonOptions);

    public ValueTask DisposeAsync()
    {
        _signaling.EnvelopeReceived -= OnSignalingEnvelope;
        _microphone?.Stop();
        _annotations?.close();
        _pc.close();
        return ValueTask.CompletedTask;
    }

    /// <summary>DTO for the RTCIceCandidateInit wire shape exchanged over signaling.</summary>
    private sealed class IceCandidateDto
    {
        [System.Text.Json.Serialization.JsonPropertyName("candidate")]
        public string? Candidate { get; set; }

        [System.Text.Json.Serialization.JsonPropertyName("sdpMid")]
        public string? SdpMid { get; set; }

        [System.Text.Json.Serialization.JsonPropertyName("sdpMLineIndex")]
        public ushort? SdpMLineIndex { get; set; }
    }
}
