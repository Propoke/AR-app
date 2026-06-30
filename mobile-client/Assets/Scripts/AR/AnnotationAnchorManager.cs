using System.Collections.Generic;
using ARApp.Protocol;
using UnityEngine;
using UnityEngine.XR.ARFoundation;
using UnityEngine.XR.ARSubsystems;

namespace ARApp.AR
{
    /// <summary>
    /// Turns incoming <see cref="AnnotationEvent"/>s into world-anchored markers.
    ///
    /// The agent sends a normalized point (u,v) on its video frame. We map it to a
    /// screen point on this device, raycast into the AR scene, and attach a
    /// persistent ARAnchor so the marker stays fixed to the real object as the phone
    /// moves. The anchor id is reported back via <see cref="AnchorEstablished"/> so
    /// the agent's annotation can be correlated with the phone's anchor.
    /// </summary>
    [RequireComponent(typeof(ARRaycastManager))]
    public sealed class AnnotationAnchorManager : MonoBehaviour
    {
        [SerializeField] private ARRaycastManager _raycastManager;
        [SerializeField] private ARAnchorManager _anchorManager;
        [SerializeField] private GameObject _markerPrefab;

        private readonly Dictionary<string, ARAnchor> _anchors = new Dictionary<string, ARAnchor>();
        private readonly List<ARRaycastHit> _hits = new List<ARRaycastHit>();

        /// <summary>Raised after an anchor is created: (annotationId, anchorId).</summary>
        public System.Action<string, string> AnchorEstablished;

        private void Awake()
        {
            if (_raycastManager == null) _raycastManager = GetComponent<ARRaycastManager>();
            if (_anchorManager == null) _anchorManager = GetComponent<ARAnchorManager>();
        }

        /// <summary>Applies an annotation operation. Call on the main thread.</summary>
        public void Apply(AnnotationEvent evt)
        {
            switch (evt.Op)
            {
                case "create":
                case "update":
                    PlaceOrMove(evt);
                    break;
                case "delete":
                    Remove(evt.Id);
                    break;
                case "clear":
                    Clear();
                    break;
            }
        }

        private void PlaceOrMove(AnnotationEvent evt)
        {
            if (evt.Point == null)
                return;

            // Normalized (u,v) with v=0 at the top maps to screen space (y up).
            var screenPoint = new Vector2(
                (float)evt.Point.U * Screen.width,
                (1f - (float)evt.Point.V) * Screen.height);

            // Prefer planes; fall back to feature points/depth for unmapped surfaces.
            if (!_raycastManager.Raycast(screenPoint, _hits,
                    TrackableType.PlaneWithinPolygon | TrackableType.FeaturePoint | TrackableType.Depth))
            {
                Debug.Log("[ar] raycast found no surface for annotation " + evt.Id);
                return;
            }

            var hit = _hits[0];

            // Replace any existing anchor for this annotation id (update case).
            Remove(evt.Id);

            var anchor = CreateAnchor(hit);
            if (anchor == null)
                return;

            var marker = Instantiate(_markerPrefab, anchor.transform);
            marker.transform.localPosition = Vector3.zero;
            ApplyStyle(marker, evt);

            _anchors[evt.Id] = anchor;
            AnchorEstablished?.Invoke(evt.Id, anchor.trackableId.ToString());
        }

        private ARAnchor CreateAnchor(ARRaycastHit hit)
        {
            // If we hit a plane, attach the anchor to it for the most stable tracking.
            if (_anchorManager != null && hit.trackable is ARPlane plane)
                return _anchorManager.AttachAnchor(plane, hit.pose);

            // Otherwise create a free-standing anchor at the hit pose.
            var go = new GameObject($"anchor-{hit.trackableId}");
            go.transform.SetPositionAndRotation(hit.pose.position, hit.pose.rotation);
            return go.AddComponent<ARAnchor>();
        }

        private static void ApplyStyle(GameObject marker, AnnotationEvent evt)
        {
            if (string.IsNullOrEmpty(evt.Color))
                return;
            if (ColorUtility.TryParseHtmlString(evt.Color, out var color))
            {
                var renderer = marker.GetComponentInChildren<Renderer>();
                if (renderer != null)
                    renderer.material.color = color;
            }
        }

        private void Remove(string annotationId)
        {
            if (_anchors.TryGetValue(annotationId, out var anchor))
            {
                if (anchor != null)
                    Destroy(anchor.gameObject);
                _anchors.Remove(annotationId);
            }
        }

        private void Clear()
        {
            foreach (var anchor in _anchors.Values)
            {
                if (anchor != null)
                    Destroy(anchor.gameObject);
            }
            _anchors.Clear();
        }
    }
}
