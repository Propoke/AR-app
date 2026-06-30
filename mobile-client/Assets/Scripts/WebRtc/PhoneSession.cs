using System;
using System.Collections;
using System.Collections.Generic;
using ARApp.Protocol;
using Unity.WebRTC;
using UnityEngine;

namespace ARApp.WebRtc
{
    /// <summary>
    /// The phone side of the WebRTC session. It sends the AR camera (rendered to a
    /// RenderTexture, encoded as VP8) and microphone audio (Opus), answers the
    /// agent's offer, and forwards annotations received over the data channel.
    ///
    /// Driven by SessionController, which owns signaling. Unity.WebRTC callbacks
    /// fire on the main thread, so AnnotationReceived is safe to consume directly.
    /// </summary>
    public sealed class PhoneSession : MonoBehaviour
    {
        private RTCPeerConnection _pc;
        private VideoStreamTrack _videoTrack;
        private AudioStreamTrack _audioTrack;

        /// <summary>Invoked with our SDP answer to relay back to the agent.</summary>
        public Action<string> OnLocalAnswer;

        /// <summary>Invoked with each local ICE candidate to relay to the agent.</summary>
        public Action<IceCandidatePayload> OnLocalIce;

        /// <summary>Raised when the agent sends an annotation to render in AR.</summary>
        public event Action<AnnotationEvent> AnnotationReceived;

        /// <summary>Raised on peer-connection state changes.</summary>
        public event Action<RTCPeerConnectionState> ConnectionStateChanged;

        private void Awake()
        {
            // Required pump for Unity.WebRTC's async work and encoders.
            StartCoroutine(WebRTC.Update());
        }

        /// <summary>
        /// Builds the peer connection with the backend's ICE servers, attaches the
        /// AR camera RenderTexture as the outgoing video track, the microphone as the
        /// outgoing audio track, and routes received audio to <paramref name="playbackSource"/>.
        /// </summary>
        public void Setup(
            List<IceServerInfo> iceServers,
            RenderTexture arCameraTexture,
            AudioSource microphoneSource,
            AudioSource playbackSource)
        {
            var config = BuildConfig(iceServers);
            _pc = new RTCPeerConnection(ref config);

            _videoTrack = new VideoStreamTrack(arCameraTexture);
            _pc.AddTrack(_videoTrack);

            // The microphone AudioSource (a looping mic clip) becomes the audio track.
            _audioTrack = new AudioStreamTrack(microphoneSource);
            _pc.AddTrack(_audioTrack);

            // Play the technician's audio through the playback AudioSource.
            _pc.OnTrack = evt =>
            {
                if (evt.Track is AudioStreamTrack incoming)
                {
                    playbackSource.SetTrack(incoming);
                    playbackSource.loop = true;
                    playbackSource.Play();
                }
            };

            _pc.OnIceCandidate = candidate =>
            {
                OnLocalIce?.Invoke(new IceCandidatePayload
                {
                    Candidate = candidate.Candidate,
                    SdpMid = candidate.SdpMid,
                    SdpMLineIndex = candidate.SdpMLineIndex,
                });
            };

            _pc.OnConnectionStateChange = state => ConnectionStateChanged?.Invoke(state);

            // The agent creates the "annotations" data channel; we receive it.
            _pc.OnDataChannel = channel =>
            {
                if (channel.Label != "annotations")
                    return;
                channel.OnMessage = bytes =>
                {
                    try
                    {
                        var json = System.Text.Encoding.UTF8.GetString(bytes);
                        AnnotationReceived?.Invoke(AnnotationEvent.Parse(json));
                    }
                    catch (Exception ex)
                    {
                        Debug.LogWarning($"[webrtc] bad annotation: {ex.Message}");
                    }
                };
            };
        }

        /// <summary>Applies the agent's offer and produces an answer.</summary>
        public IEnumerator HandleOffer(string offerSdp)
        {
            var offer = new RTCSessionDescription { type = RTCSdpType.Offer, sdp = offerSdp };
            var setRemote = _pc.SetRemoteDescription(ref offer);
            yield return setRemote;
            if (setRemote.IsError)
            {
                Debug.LogError($"[webrtc] setRemoteDescription: {setRemote.Error.message}");
                yield break;
            }

            var createAnswer = _pc.CreateAnswer();
            yield return createAnswer;
            if (createAnswer.IsError)
            {
                Debug.LogError($"[webrtc] createAnswer: {createAnswer.Error.message}");
                yield break;
            }

            var answer = createAnswer.Desc;
            var setLocal = _pc.SetLocalDescription(ref answer);
            yield return setLocal;
            if (setLocal.IsError)
            {
                Debug.LogError($"[webrtc] setLocalDescription: {setLocal.Error.message}");
                yield break;
            }

            OnLocalAnswer?.Invoke(answer.sdp);
        }

        /// <summary>Adds a remote ICE candidate from the agent.</summary>
        public void AddIceCandidate(IceCandidatePayload payload)
        {
            if (string.IsNullOrEmpty(payload?.Candidate))
                return;
            var init = new RTCIceCandidateInit
            {
                candidate = payload.Candidate,
                sdpMid = payload.SdpMid,
                sdpMLineIndex = payload.SdpMLineIndex,
            };
            _pc.AddIceCandidate(new RTCIceCandidate(init));
        }

        private static RTCConfiguration BuildConfig(List<IceServerInfo> iceServers)
        {
            var servers = new List<RTCIceServer>();
            if (iceServers != null)
            {
                foreach (var s in iceServers)
                {
                    servers.Add(new RTCIceServer
                    {
                        urls = s.Urls?.ToArray() ?? Array.Empty<string>(),
                        username = s.Username,
                        credential = s.Credential,
                    });
                }
            }
            return new RTCConfiguration { iceServers = servers.ToArray() };
        }

        private void OnDestroy()
        {
            _videoTrack?.Dispose();
            _audioTrack?.Dispose();
            _pc?.Close();
            _pc?.Dispose();
        }
    }
}
