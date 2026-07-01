#if UNITY_EDITOR
using ARApp;
using ARApp.AR;
using ARApp.UI;
using ARApp.WebRtc;
using Unity.XR.CoreUtils;
using UnityEditor;
using UnityEngine;
using UnityEngine.UI;
using UnityEngine.XR.ARFoundation;

namespace ARApp.EditorTools
{
    /// <summary>
    /// One-click wiring for the session scene. It reduces the manual setup in
    /// docs/first-session.md to: create an "XR Origin (AR)" and an "AR Session"
    /// from the GameObject > XR menu (Unity builds those correctly), then run
    /// <b>AR App ▸ Wire Session Scene</b>. This adds the AR managers, the camera
    /// streamer, a marker prefab, the token-entry UI, and the controller graph,
    /// wiring all references.
    ///
    /// It is a scaffold: review the result in the Inspector and adjust visuals.
    /// It intentionally does NOT build the XR Origin hierarchy itself, since that
    /// is version-sensitive and the menu command is reliable.
    /// </summary>
    public static class ARSceneBootstrap
    {
        private const string MarkerPrefabPath = "Assets/Prefabs/AnnotationMarker.prefab";

        [MenuItem("AR App/Wire Session Scene")]
        public static void WireScene()
        {
            var origin = Object.FindObjectOfType<XROrigin>();
            if (origin == null)
            {
                EditorUtility.DisplayDialog("Missing XR Origin",
                    "Create an AR rig first:\n\n" +
                    "1. GameObject ▸ XR ▸ XR Origin (AR)\n" +
                    "2. GameObject ▸ XR ▸ AR Session\n\n" +
                    "Then run AR App ▸ Wire Session Scene again.", "OK");
                return;
            }

            // AR managers on the origin.
            var raycast = Ensure<ARRaycastManager>(origin.gameObject);
            var anchors = Ensure<ARAnchorManager>(origin.gameObject);
            Ensure<ARPlaneManager>(origin.gameObject);

            // Stream the AR camera background as the outgoing video.
            if (origin.Camera != null)
                Ensure<ARCameraBackground>(origin.Camera.gameObject);
            var streamer = origin.Camera != null
                ? Ensure<ARCameraStreamer>(origin.Camera.gameObject)
                : null;

            var markerPrefab = EnsureMarkerPrefab();

            // UI: canvas + event system + token entry + 2D overlay.
            EnsureEventSystem();
            var canvas = CreateCanvas();
            var overlayRoot = CreateOverlayRoot(canvas);
            var overlay = overlayRoot.gameObject.AddComponent<Annotation2DOverlay>();
            var overlayMarker = CreateOverlayMarker(canvas.transform);
            Wire(overlay, "_canvas", overlayRoot);
            Wire(overlay, "_markerPrefab", overlayMarker);

            var (panel, idField, pinField, connectButton, status) = CreateTokenEntry(canvas);

            // Controllers.
            var controllers = new GameObject("Session");
            var phone = controllers.AddComponent<PhoneSession>();
            var anchorMgr = controllers.AddComponent<AnnotationAnchorManager>();
            var controller = controllers.AddComponent<SessionController>();

            var micSource = CreateAudioSource("MicrophoneSource");
            var playbackSource = CreateAudioSource("PlaybackSource");

            // Wire AnnotationAnchorManager.
            Wire(anchorMgr, "_raycastManager", raycast);
            Wire(anchorMgr, "_anchorManager", anchors);
            Wire(anchorMgr, "_markerPrefab", markerPrefab);
            Wire(anchorMgr, "_overlay", overlay);

            // Wire SessionController.
            Wire(controller, "_cameraStreamer", streamer);
            Wire(controller, "_microphoneSource", micSource);
            Wire(controller, "_playbackSource", playbackSource);
            Wire(controller, "_session", phone);
            Wire(controller, "_anchorManager", anchorMgr);

            // Wire TokenEntryUI.
            var tokenUI = panel.AddComponent<TokenEntryUI>();
            Wire(tokenUI, "_controller", controller);
            Wire(tokenUI, "_connectIdField", idField);
            Wire(tokenUI, "_pinField", pinField);
            Wire(tokenUI, "_connectButton", connectButton);
            Wire(tokenUI, "_status", status);
            Wire(tokenUI, "_panel", panel);

            Selection.activeObject = controllers;
            EditorUtility.DisplayDialog("Scene wired",
                "Added AR managers, camera streamer, marker prefab, token-entry UI, " +
                "and the controller graph.\n\nSet the backend URL on the Session " +
                "controller, then build to a device (see docs/first-session.md).", "OK");
        }

        private static T Ensure<T>(GameObject go) where T : Component
        {
            var c = go.GetComponent<T>();
            return c != null ? c : go.AddComponent<T>();
        }

        // Sets a private [SerializeField] reference by field name.
        private static void Wire(Object target, string field, Object value)
        {
            var so = new SerializedObject(target);
            var prop = so.FindProperty(field);
            if (prop == null)
            {
                Debug.LogWarning($"ARSceneBootstrap: '{field}' not found on {target.GetType().Name}");
                return;
            }
            prop.objectReferenceValue = value;
            so.ApplyModifiedPropertiesWithoutUndo();
        }

        private static GameObject EnsureMarkerPrefab()
        {
            var existing = AssetDatabase.LoadAssetAtPath<GameObject>(MarkerPrefabPath);
            if (existing != null)
                return existing;

            System.IO.Directory.CreateDirectory("Assets/Prefabs");
            var sphere = GameObject.CreatePrimitive(PrimitiveType.Sphere);
            sphere.name = "AnnotationMarker";
            sphere.transform.localScale = Vector3.one * 0.05f; // ~5 cm
            var prefab = PrefabUtility.SaveAsPrefabAsset(sphere, MarkerPrefabPath);
            Object.DestroyImmediate(sphere);
            return prefab;
        }

        private static void EnsureEventSystem()
        {
            if (Object.FindObjectOfType<UnityEngine.EventSystems.EventSystem>() != null)
                return;
            var es = new GameObject("EventSystem");
            es.AddComponent<UnityEngine.EventSystems.EventSystem>();
            es.AddComponent<UnityEngine.EventSystems.StandaloneInputModule>();
        }

        private static Canvas CreateCanvas()
        {
            var go = new GameObject("UI Canvas");
            var canvas = go.AddComponent<Canvas>();
            canvas.renderMode = RenderMode.ScreenSpaceOverlay;
            go.AddComponent<CanvasScaler>();
            go.AddComponent<GraphicRaycaster>();
            return canvas;
        }

        private static RectTransform CreateOverlayRoot(Canvas canvas)
        {
            var go = new GameObject("Annotation Overlay", typeof(RectTransform));
            var rt = go.GetComponent<RectTransform>();
            rt.SetParent(canvas.transform, false);
            Stretch(rt);
            return rt;
        }

        private static RectTransform CreateOverlayMarker(Transform parent)
        {
            var go = new GameObject("OverlayMarker", typeof(RectTransform), typeof(Image));
            var rt = go.GetComponent<RectTransform>();
            rt.SetParent(parent, false);
            rt.sizeDelta = new Vector2(48, 48);
            go.GetComponent<Image>().color = new Color(1f, 0.23f, 0.19f); // #ff3b30
            go.SetActive(true);
            return rt;
        }

        private static (GameObject panel, InputField id, InputField pin, Button connect, Text status)
            CreateTokenEntry(Canvas canvas)
        {
            var panel = new GameObject("Token Entry", typeof(RectTransform), typeof(Image));
            var prt = panel.GetComponent<RectTransform>();
            prt.SetParent(canvas.transform, false);
            prt.anchorMin = new Vector2(0.5f, 0.5f);
            prt.anchorMax = new Vector2(0.5f, 0.5f);
            prt.sizeDelta = new Vector2(400, 300);
            panel.GetComponent<Image>().color = new Color(0f, 0f, 0f, 0.6f);

            var id = CreateInputField(prt, "Connect ID", new Vector2(0, 80));
            var pin = CreateInputField(prt, "PIN", new Vector2(0, 20));
            var connect = CreateButton(prt, "Connect", new Vector2(0, -50));
            var status = CreateText(prt, "Enter the ID and PIN", new Vector2(0, -110));
            return (panel, id, pin, connect, status);
        }

        private static InputField CreateInputField(Transform parent, string placeholder, Vector2 pos)
        {
            var go = new GameObject(placeholder + " Field", typeof(RectTransform), typeof(Image), typeof(InputField));
            var rt = go.GetComponent<RectTransform>();
            rt.SetParent(parent, false);
            rt.anchoredPosition = pos;
            rt.sizeDelta = new Vector2(320, 40);

            var text = CreateText(rt, "", Vector2.zero);
            var ph = CreateText(rt, placeholder, Vector2.zero);
            ph.color = new Color(1, 1, 1, 0.5f);

            var input = go.GetComponent<InputField>();
            input.textComponent = text;
            input.placeholder = ph;
            return input;
        }

        private static Button CreateButton(Transform parent, string label, Vector2 pos)
        {
            var go = new GameObject(label + " Button", typeof(RectTransform), typeof(Image), typeof(Button));
            var rt = go.GetComponent<RectTransform>();
            rt.SetParent(parent, false);
            rt.anchoredPosition = pos;
            rt.sizeDelta = new Vector2(200, 44);
            go.GetComponent<Image>().color = new Color(0.2f, 0.5f, 0.9f);
            CreateText(rt, label, Vector2.zero);
            return go.GetComponent<Button>();
        }

        private static Text CreateText(Transform parent, string content, Vector2 pos)
        {
            var go = new GameObject("Text", typeof(RectTransform), typeof(Text));
            var rt = go.GetComponent<RectTransform>();
            rt.SetParent(parent, false);
            rt.anchoredPosition = pos;
            rt.sizeDelta = new Vector2(320, 40);
            var text = go.GetComponent<Text>();
            text.text = content;
            text.alignment = TextAnchor.MiddleCenter;
            text.color = Color.white;
            text.font = Resources.GetBuiltinResource<Font>("LegacyRuntime.ttf");
            return text;
        }

        private static AudioSource CreateAudioSource(string name)
        {
            var go = new GameObject(name);
            var src = go.AddComponent<AudioSource>();
            src.playOnAwake = false;
            return src;
        }

        private static void Stretch(RectTransform rt)
        {
            rt.anchorMin = Vector2.zero;
            rt.anchorMax = Vector2.one;
            rt.offsetMin = Vector2.zero;
            rt.offsetMax = Vector2.zero;
        }
    }
}
#endif
