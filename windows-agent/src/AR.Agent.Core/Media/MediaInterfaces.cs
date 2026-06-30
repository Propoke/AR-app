namespace AR.Agent.Core.Media;

/// <summary>A decoded video frame ready to display (BGRA32, top-down).</summary>
public readonly record struct VideoFrame(int Width, int Height, byte[] Bgra);

/// <summary>
/// Renders decoded video frames from the phone's camera. Implemented by the UI
/// layer (e.g. a WinUI WriteableBitmap). Kept here so SessionConnection has no
/// dependency on a windowing toolkit.
/// </summary>
public interface IVideoRenderer
{
    void Render(VideoFrame frame);
}

/// <summary>
/// A microphone that produces encoded (Opus) audio samples. The duration is in
/// RTP timestamp units for the negotiated clock (48 kHz for Opus).
/// </summary>
public interface IMicrophone
{
    event Action<uint, byte[]> EncodedSampleReady;
    void Start();
    void Stop();
}

/// <summary>A speaker that plays encoded (Opus) audio payloads received from the peer.</summary>
public interface ISpeaker
{
    void PlayEncoded(byte[] opusPayload);
}

/// <summary>
/// Decodes encoded video samples (VP8) into displayable frames. Implemented with a
/// native codec in the Windows layer; abstracted here to keep the core portable.
/// </summary>
public interface IVideoDecoder
{
    /// <summary>Returns a decoded frame, or null if more data is needed.</summary>
    VideoFrame? Decode(byte[] encodedSample);
}
