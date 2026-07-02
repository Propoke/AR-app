using AR.Agent.Core.Media;
using Microsoft.UI.Dispatching;
using Microsoft.UI.Xaml.Media.Imaging;
using SIPSorcery.Media;
using SIPSorcery.Net;
using SIPSorceryMedia.Abstractions;
using SIPSorceryMedia.Encoders;
using SIPSorceryMedia.Windows;
// WindowsRuntimeBufferExtensions.AsStream(this IBuffer) — needed to turn
// WriteableBitmap.PixelBuffer (a WinRT IBuffer) into a managed Stream below.
// Without this using, the compiler only sees the unrelated
// WindowsRuntimeStreamExtensions.AsStream(IRandomAccessStream) overload.
using System.Runtime.InteropServices.WindowsRuntime;

namespace AR.Agent.App.Media;

// Windows implementations of the AR.Agent.Core.Media interfaces. This is the
// platform integration surface; it uses native codecs and audio devices and is
// only built/run on Windows. API details are finalized during the on-device
// Windows build pass (see docs/verification.md).

/// <summary>
/// Microphone + speaker backed by a single Windows audio endpoint. Produces
/// encoded Opus samples for sending and plays received Opus payloads.
/// </summary>
public sealed class WindowsAudioDevice : IMicrophone, ISpeaker
{
    private readonly WindowsAudioEndPoint _endpoint;
    private uint _sinkTimestamp;

    public event Action<uint, byte[]>? EncodedSampleReady;

    public WindowsAudioDevice()
    {
        _endpoint = new WindowsAudioEndPoint(new AudioEncoder());
        _endpoint.RestrictFormats(f => f.Codec == AudioCodecsEnum.OPUS);
        _endpoint.OnAudioSourceEncodedSample += (durationRtpUnits, sample) =>
            EncodedSampleReady?.Invoke(durationRtpUnits, sample);
    }

    public void Start()
    {
        _ = _endpoint.StartAudio();
        _ = _endpoint.StartAudioSink();
    }

    public void Stop()
    {
        _ = _endpoint.CloseAudio();
        _ = _endpoint.CloseAudioSink();
    }

    public void PlayEncoded(int payloadType, byte[] opusPayload)
    {
        // Feed the sink's jitter buffer with the RTP packet's ACTUAL negotiated
        // payload type (passed through from SessionConnection's received RTP
        // header), not a hardcoded guess. An earlier version of this method
        // hardcoded SDPWellKnownMediaFormatsEnum.PCMU here with a comment
        // claiming it would be "overridden by negotiated Opus format" — there
        // is no such override mechanism in this API; that would have told the
        // sink to decode Opus-encoded bytes as PCMU (an unrelated, incompatible
        // codec), producing silence or garbage/noise instead of audio.
        // Sequence/SSRC are not significant for a single inbound stream; the
        // timestamp advances by a 20 ms Opus frame.
        _sinkTimestamp += 960;
        _endpoint.GotAudioRtp(
            remoteEndPoint: null,
            ssrc: 0,
            seqnum: 0,
            timestamp: _sinkTimestamp,
            payloadID: payloadType,
            marker: false,
            payload: opusPayload);
    }
}

/// <summary>Decodes VP8 encoded samples to BGRA frames using the native VPX codec.</summary>
public sealed class Vp8VideoDecoder : IVideoDecoder
{
    // VpxVideoEncoder (SIPSorceryMedia.Encoders namespace) — confirmed against
    // the package source: it was renamed from VideoEncoder to VpxVideoEncoder
    // upstream (sipsorcery-org/SIPSorceryMedia.Encoders, VpxVideoEncoder.cs).
    private readonly VpxVideoEncoder _codec = new();

    public VideoFrame? Decode(byte[] encodedSample)
    {
        foreach (var raw in _codec.DecodeVideo(encodedSample, VideoPixelFormatsEnum.Bgra, VideoCodecsEnum.VP8))
        {
            // Take the first decoded frame; VP8 yields at most one per sample here.
            return new VideoFrame((int)raw.Width, (int)raw.Height, raw.Sample);
        }
        return null;
    }
}

/// <summary>Renders decoded frames into a WriteableBitmap on the UI thread.</summary>
public sealed class WriteableBitmapRenderer : IVideoRenderer
{
    private readonly DispatcherQueue _dispatcher;
    private readonly Action<WriteableBitmap> _onBitmap;
    private WriteableBitmap? _bitmap;

    public WriteableBitmapRenderer(DispatcherQueue dispatcher, Action<WriteableBitmap> onBitmap)
    {
        _dispatcher = dispatcher;
        _onBitmap = onBitmap;
    }

    public void Render(VideoFrame frame)
    {
        _dispatcher.TryEnqueue(() =>
        {
            if (_bitmap is null || _bitmap.PixelWidth != frame.Width || _bitmap.PixelHeight != frame.Height)
            {
                _bitmap = new WriteableBitmap(frame.Width, frame.Height);
                _onBitmap(_bitmap);
            }
            using var stream = _bitmap.PixelBuffer.AsStream();
            stream.Write(frame.Bgra, 0, frame.Bgra.Length);
            _bitmap.Invalidate();
        });
    }
}
