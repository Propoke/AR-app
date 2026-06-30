using AR.Agent.Core.Media;
using Microsoft.UI.Dispatching;
using Microsoft.UI.Xaml.Media.Imaging;
using SIPSorcery.Net;
using SIPSorceryMedia.Abstractions;
using SIPSorceryMedia.Encoders;
using SIPSorceryMedia.Windows;

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

    public void PlayEncoded(byte[] opusPayload)
    {
        // Feed the sink's jitter buffer. Sequence/SSRC are not significant for a
        // single inbound stream; the timestamp advances by a 20 ms Opus frame.
        _sinkTimestamp += 960;
        _endpoint.GotAudioRtp(
            remoteEndPoint: null,
            ssrc: 0,
            seqnum: 0,
            timestamp: _sinkTimestamp,
            payloadID: (int)SDPWellKnownMediaFormatsEnum.PCMU, // overridden by negotiated Opus format
            marker: false,
            payload: opusPayload);
    }
}

/// <summary>Decodes VP8 encoded samples to BGRA frames using the native VPX codec.</summary>
public sealed class Vp8VideoDecoder : IVideoDecoder
{
    private readonly VideoEncoder _codec = new();

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
