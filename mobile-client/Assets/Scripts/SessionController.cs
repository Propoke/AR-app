using System;
using System.Collections;
using System.Collections.Concurrent;
using System.Text;
using ARApp.AR;
using ARApp.Protocol;
using ARApp.Signaling;
using ARApp.WebRtc;
using Newtonsoft.Json;
using Unity.WebRTC;
using UnityEngine;
using UnityEngine.Networking;

namespace ARApp
{
    /// <summary>
    /// Top-level flow for the end-user app: redeem a connection token, connect
    /// signaling, answer the agent's WebRTC offer, and route incoming annotations
    /// to the AR anchor manager. Echoes established anchor ids back to the agent.
    /// </summary>
    public sealed class SessionController : MonoBehaviour
    {
        [Header("Backend")]
        [SerializeField] private string _httpBaseUrl = "http://localhost:8080";
        [SerializeField] private string _wsBaseUrl = "ws://localhost:8080";

        [Header("Scene wiring")]
        [SerializeField] private Camera _arCamera;
        [SerializeField] private PhoneSession _session;
        [SerializeField] private AnnotationAnchorManager _anchorManager;

        private SignalingClient _signaling;
        private RenderTexture _cameraTexture;
        private readonly ConcurrentQueue<Action> _mainThread = new ConcurrentQueue<Action>();

        /// <summary>Entry point from the token-entry UI.</summary>
        public void Connect(string connectId, string pin)
        {
            StartCoroutine(RedeemAndConnect(connectId, pin));
        }

        private IEnumerator RedeemAndConnect(string connectId, string pin)
        {
            var body = JsonConvert.SerializeObject(new { connect_id = connectId, pin });
            using var req = new UnityWebRequest($"{_httpBaseUrl}/v1/sessions/redeem", "POST")
            {
                uploadHandler = new UploadHandlerRaw(Encoding.UTF8.GetBytes(body)),
                downloadHandler = new DownloadHandlerBuffer(),
            };
            req.SetRequestHeader("Content-Type", "application/json");
            yield return req.SendWebRequest();

            if (req.result != UnityWebRequest.Result.Success)
            {
                Debug.LogError($"[session] redeem failed: {req.responseCode} {req.error}");
                yield break;
            }

            var join = JsonConvert.DeserializeObject<JoinInfo>(req.downloadHandler.text);
            StartSession(join);
        }

        private void StartSession(JoinInfo join)
        {
            // Render the AR camera into a texture to use as the outgoing video track.
            // (Blitting the ARCameraBackground into this texture is finalized in the
            //  AR scene setup; the track streams whatever this RenderTexture holds.)
            _cameraTexture = new RenderTexture(1280, 720, 0, RenderTextureFormat.BGRA32);
            _cameraTexture.Create();
            if (_arCamera != null)
                _arCamera.targetTexture = _cameraTexture;

            _session.Setup(join.IceServers, _cameraTexture);

            _session.OnLocalAnswer = sdp =>
                _ = _signaling.SendAsync("answer", new SdpPayload { Sdp = sdp });
            _session.OnLocalIce = ice =>
                _ = _signaling.SendAsync("ice", ice);

            _session.AnnotationReceived += evt => _mainThread.Enqueue(() => _anchorManager.Apply(evt));

            // When an anchor is established, echo it back so the agent can correlate.
            _anchorManager.AnchorEstablished = (annotationId, anchorId) =>
                _ = _signaling.SendAsync("annotation", new AnnotationEvent
                {
                    Op = "update",
                    Id = annotationId,
                    AnchorId = anchorId,
                });

            _signaling = new SignalingClient(new Uri(_wsBaseUrl));
            _signaling.EnvelopeReceived += env => _mainThread.Enqueue(() => HandleEnvelope(env));
            _ = _signaling.ConnectAsync(join.Room);
        }

        private void HandleEnvelope(SignalingEnvelope env)
        {
            switch (env.Type)
            {
                case "offer":
                    var sdp = ToObject<SdpPayload>(env.Payload);
                    if (sdp != null)
                        StartCoroutine(_session.HandleOffer(sdp.Sdp));
                    break;
                case "ice":
                    var ice = ToObject<IceCandidatePayload>(env.Payload);
                    if (ice != null)
                        _session.AddIceCandidate(ice);
                    break;
                case "bye":
                    Debug.Log("[session] agent disconnected");
                    break;
            }
        }

        private void Update()
        {
            while (_mainThread.TryDequeue(out var action))
                action();
        }

        private void OnDestroy()
        {
            _signaling?.Dispose();
            if (_cameraTexture != null)
            {
                if (_arCamera != null) _arCamera.targetTexture = null;
                _cameraTexture.Release();
            }
        }

        // env.Payload arrives as a Newtonsoft JObject; convert to the concrete type.
        private static T ToObject<T>(object payload) where T : class
        {
            if (payload == null) return null;
            if (payload is Newtonsoft.Json.Linq.JObject jo) return jo.ToObject<T>();
            return JsonConvert.DeserializeObject<T>(JsonConvert.SerializeObject(payload));
        }
    }
}
