using System;
using System.Threading.Tasks;
using AR.Agent.App.Media;
using AR.Agent.Core;
using AR.Agent.Core.Protocol;
using Microsoft.UI.Dispatching;
using Microsoft.UI.Xaml;
using SIPSorcery.Net;

namespace AR.Agent.App;

/// <summary>
/// Technician window. Phase 2 wires the end-to-end flow: sign in, mint a token,
/// connect signaling, and establish the WebRTC session. Live video rendering and
/// the annotation toolbar arrive in later phases; this shell proves the plumbing.
/// </summary>
public sealed partial class MainWindow : Window
{
    private readonly DispatcherQueue _dispatcher;

    private BackendClient? _backend;
    private Uri _wsBase = ServerConfig.WebSocketBase(ServerConfig.ResolveHttpBase());
    private SignalingClient? _signaling;
    private SessionConnection? _session;

    public MainWindow()
    {
        InitializeComponent();
        _dispatcher = DispatcherQueue.GetForCurrentThread();
        // Prefill the server field from the env var / default.
        ServerBox.Text = ServerConfig.ResolveHttpBase().ToString();
    }

    private async void OnTestConnectionClick(object sender, RoutedEventArgs e)
    {
        try
        {
            var httpBase = ServerConfig.ResolveHttpBase(ServerBox.Text);
            SetStatus($"Testing {httpBase}…");
            var ok = await new BackendClient(httpBase).CheckHealthAsync();
            SetStatus(ok ? $"Reachable: {httpBase}" : $"Not reachable: {httpBase}");
        }
        catch (Exception ex)
        {
            SetStatus($"Bad server URL: {ex.Message}");
        }
    }

    private async void OnLoginClick(object sender, RoutedEventArgs e)
    {
        try
        {
            // Resolve the backend from the server field; derive the signaling URL.
            var httpBase = ServerConfig.ResolveHttpBase(ServerBox.Text);
            _wsBase = ServerConfig.WebSocketBase(httpBase);
            _backend = new BackendClient(httpBase);

            SetStatus("Signing in…");
            await _backend.LoginAsync(EmailBox.Text, PasswordBox.Password);
            StartButton.IsEnabled = true;
            SetStatus("Signed in. Start a session to generate a connection code.");
        }
        catch (Exception ex)
        {
            SetStatus($"Sign-in failed: {ex.Message}");
        }
    }

    private async void OnStartSessionClick(object sender, RoutedEventArgs e)
    {
        try
        {
            if (_backend is null)
            {
                SetStatus("Sign in first.");
                return;
            }
            StartButton.IsEnabled = false;
            SetStatus("Creating session…");

            var minted = await _backend.MintSessionAsync();
            ConnectIdText.Text = $"ID {minted.ConnectId}";
            PinText.Text = $"PIN {minted.Pin}";
            SetStatus("Waiting for the end-user to enter the code on their phone…");

            _signaling = new SignalingClient(_wsBase);
            _signaling.EnvelopeReceived += OnEnvelope;
            await _signaling.ConnectAsync(minted.Room, minted.SignalingToken);
            _ = Task.Run(() => _signaling.ReceiveLoopAsync());

            _session = new SessionConnection(_signaling, minted.IceServers);
            _session.ConnectionStateChanged += OnConnectionStateChanged;
            _session.AnnotationReceived += OnAnnotationReceived;

            // Attach Windows audio + VP8 decode so the phone's camera renders in the
            // panel and two-way audio flows.
            var audio = new WindowsAudioDevice();
            var renderer = new WriteableBitmapRenderer(_dispatcher, bmp => VideoImage.Source = bmp);
            _session.AttachMedia(audio, audio, new Vp8VideoDecoder(), renderer);
        }
        catch (Exception ex)
        {
            SetStatus($"Could not start session: {ex.Message}");
            StartButton.IsEnabled = true;
        }
    }

    private async void OnEnvelope(SignalingEnvelope env)
    {
        // Once the phone is present, send our offer to begin negotiation.
        if (env.Type == "peer-ready" && _session is not null)
        {
            try
            {
                await _session.StartAsync();
            }
            catch (Exception ex)
            {
                SetStatus($"Negotiation failed: {ex.Message}");
            }
        }
    }

    private void OnVideoPointerPressed(object sender, Microsoft.UI.Xaml.Input.PointerRoutedEventArgs e)
    {
        if (_session is null)
            return;

        // Convert the click to normalized (u,v) over the source frame, accounting
        // for the letterbox bars of the uniform-fit image. Without the source size
        // (no frame yet), fall back to control-relative mapping.
        var point = e.GetCurrentPoint(VideoImage).Position;
        (double U, double V)? norm;
        if (VideoImage.Source is Microsoft.UI.Xaml.Media.Imaging.WriteableBitmap bmp)
        {
            norm = AR.Agent.Core.Geometry.VideoCoords.PointerToNormalized(
                point.X, point.Y, VideoImage.ActualWidth, VideoImage.ActualHeight,
                bmp.PixelWidth, bmp.PixelHeight);
        }
        else if (VideoImage.ActualWidth > 0 && VideoImage.ActualHeight > 0)
        {
            norm = (Math.Clamp(point.X / VideoImage.ActualWidth, 0, 1),
                    Math.Clamp(point.Y / VideoImage.ActualHeight, 0, 1));
        }
        else
        {
            norm = null;
        }

        if (norm is not { } uv)
            return; // clicked outside the displayed frame

        var annotation = new AnnotationEvent
        {
            Op = "create",
            Id = Guid.NewGuid().ToString(),
            Kind = SelectedTool(),
            Point = new NormPoint { U = uv.U, V = uv.V },
            Color = "#ff3b30",
        };

        try
        {
            _session.SendAnnotation(annotation);
            SetStatus($"Placed {annotation.Kind} at ({uv.U:F2}, {uv.V:F2}); awaiting anchor…");
        }
        catch (Exception ex)
        {
            SetStatus($"Could not send annotation: {ex.Message}");
        }
    }

    private void OnClearAnnotations(object sender, RoutedEventArgs e)
    {
        try
        {
            _session?.SendAnnotation(new AnnotationEvent { Op = "clear", Id = Guid.NewGuid().ToString() });
        }
        catch (Exception ex)
        {
            SetStatus($"Could not clear: {ex.Message}");
        }
    }

    private string SelectedTool()
    {
        if (CircleTool.IsChecked == true) return "circle";
        if (TextTool.IsChecked == true) return "text";
        if (MarkerTool.IsChecked == true) return "marker";
        return "arrow";
    }

    private void OnConnectionStateChanged(RTCPeerConnectionState state) =>
        SetStatus($"Connection: {state}");

    private void OnAnnotationReceived(AnnotationEvent annotation) =>
        SetStatus($"Annotation echoed: {annotation.Op} {annotation.Kind} (anchor {annotation.AnchorId})");

    private void SetStatus(string text) =>
        _dispatcher.TryEnqueue(() => StatusText.Text = text);
}
