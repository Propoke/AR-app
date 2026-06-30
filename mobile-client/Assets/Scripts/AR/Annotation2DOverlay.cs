using System.Collections.Generic;
using ARApp.Protocol;
using UnityEngine;

namespace ARApp.AR
{
    /// <summary>
    /// Draws annotations as flat markers on a screen-space canvas at the normalized
    /// (u,v) position — the Phase 4 telestration path. Used as a fallback by
    /// <see cref="AnnotationAnchorManager"/> when no AR surface is found, so the
    /// technician's guidance is always visible even before a world anchor exists.
    /// </summary>
    public sealed class Annotation2DOverlay : MonoBehaviour
    {
        [SerializeField] private RectTransform _canvas;
        [SerializeField] private RectTransform _markerPrefab; // a UI element (e.g. Image)

        private readonly Dictionary<string, RectTransform> _markers = new Dictionary<string, RectTransform>();

        public void Apply(AnnotationEvent evt)
        {
            switch (evt.Op)
            {
                case "create":
                case "update":
                    if (evt.Point != null) PlaceOrMove(evt);
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
            if (!_markers.TryGetValue(evt.Id, out var marker) || marker == null)
            {
                marker = Instantiate(_markerPrefab, _canvas);
                _markers[evt.Id] = marker;
            }

            var size = _canvas.rect.size;
            // Canvas anchored position: origin is the canvas centre, y is up.
            marker.anchoredPosition = new Vector2(
                ((float)evt.Point.U - 0.5f) * size.x,
                (0.5f - (float)evt.Point.V) * size.y);

            if (!string.IsNullOrEmpty(evt.Color) &&
                ColorUtility.TryParseHtmlString(evt.Color, out var color))
            {
                var graphic = marker.GetComponentInChildren<UnityEngine.UI.Graphic>();
                if (graphic != null) graphic.color = color;
            }
        }

        private void Remove(string id)
        {
            if (_markers.TryGetValue(id, out var marker))
            {
                if (marker != null) Destroy(marker.gameObject);
                _markers.Remove(id);
            }
        }

        private void Clear()
        {
            foreach (var marker in _markers.Values)
                if (marker != null) Destroy(marker.gameObject);
            _markers.Clear();
        }
    }
}
