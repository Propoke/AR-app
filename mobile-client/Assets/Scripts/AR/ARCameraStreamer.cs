using UnityEngine;
using UnityEngine.XR.ARFoundation;

namespace ARApp.AR
{
    /// <summary>
    /// Copies the live AR camera background into a RenderTexture each frame so it
    /// can be streamed as the outgoing WebRTC video track, without taking over the
    /// camera's on-screen output (the user still sees the AR view).
    ///
    /// It blits through the ARCameraBackground material, which holds the device
    /// camera image; this is the standard way to capture the AR feed for encoding.
    /// </summary>
    [RequireComponent(typeof(ARCameraBackground))]
    public sealed class ARCameraStreamer : MonoBehaviour
    {
        private ARCameraBackground _background;
        private RenderTexture _target;

        private void Awake() => _background = GetComponent<ARCameraBackground>();

        /// <summary>Begins copying the AR feed into <paramref name="target"/>.</summary>
        public void Begin(RenderTexture target) => _target = target;

        public void Stop() => _target = null;

        private void LateUpdate()
        {
            if (_target == null || _background == null || _background.material == null)
                return;

            // Source is null: the ARCameraBackground material samples the device
            // camera textures internally and writes the feed into the target.
            Graphics.Blit(null, _target, _background.material);
        }
    }
}
