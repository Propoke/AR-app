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
    // For local development. In production these come from app configuration.
    private static readonly Uri BackendBaseUrl = new("http://localhost:8080");
    private static readonly Uri SignalingBaseUrl = new("ws://localhost:8080");

    private readonly BackendClient _backend = new(BackendBaseUrl);
    private readonly DispatcherQueue _dispatcher;

    private SignalingClient? _signaling;
    private SessionConnection? _session;

    public MainWindow()
    {
        InitializeComponent();
        _dispatcher = DispatcherQueue.GetForCurrentThread();
    }

    private async void OnLoginClick(object sender, RoutedEventArgs e)
    {
        try
        {
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
            StartButton.IsEnabled = false;
            SetStatus("Creating session…");

            var minted = await _backend.MintSessionAsync();
            ConnectIdText.Text = $"ID {minted.ConnectId}";
            PinText.Text = $"PIN {minted.Pin}";
            SetStatus("Waiting for the end-user to enter the code on their phone…");

            _signaling = new SignalingClient(SignalingBaseUrl);
            _signaling.EnvelopeReceived += OnEnvelope;
            await _signaling.ConnectAsync(minted.Room);
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

    private void OnConnectionStateChanged(RTCPeerConnectionState state) =>
        SetStatus($"Connection: {state}");

    private void OnAnnotationReceived(AnnotationEvent annotation) =>
        SetStatus($"Annotation echoed: {annotation.Op} {annotation.Kind} (anchor {annotation.AnchorId})");

    private void SetStatus(string text) =>
        _dispatcher.TryEnqueue(() => StatusText.Text = text);
}
