using UnityEngine;
using UnityEngine.UI;

namespace ARApp.UI
{
    /// <summary>
    /// Drives the connect screen: the end-user types the ID + PIN the technician
    /// read out, taps Connect, and this hands them to the SessionController. Wire
    /// the fields, button, and status Text in the Inspector.
    ///
    /// Uses Unity's built-in UI (UnityEngine.UI). If your project uses TextMeshPro,
    /// swap InputField/Text for TMP_InputField/TMP_Text.
    /// </summary>
    public sealed class TokenEntryUI : MonoBehaviour
    {
        [SerializeField] private SessionController _controller;
        [SerializeField] private InputField _connectIdField;
        [SerializeField] private InputField _pinField;
        [SerializeField] private Button _connectButton;
        [SerializeField] private Text _status;
        [Tooltip("Root panel to hide once a session starts (optional).")]
        [SerializeField] private GameObject _panel;

        private void Awake()
        {
            if (_connectButton != null)
                _connectButton.onClick.AddListener(OnConnectClicked);
        }

        private void OnConnectClicked()
        {
            var id = _connectIdField != null ? _connectIdField.text.Trim() : "";
            var pin = _pinField != null ? _pinField.text.Trim() : "";

            if (id.Length == 0 || pin.Length == 0)
            {
                SetStatus("Enter the ID and PIN shown on the technician's screen.");
                return;
            }
            if (_controller == null)
            {
                SetStatus("Not configured: assign a SessionController.");
                return;
            }

            SetStatus("Connecting…");
            SetInteractable(false);
            _controller.Connect(id, pin);

            // The SessionController logs connection state; hide the panel so the AR
            // view is unobstructed once we've handed off.
            if (_panel != null)
                _panel.SetActive(false);
        }

        private void SetInteractable(bool value)
        {
            if (_connectButton != null) _connectButton.interactable = value;
            if (_connectIdField != null) _connectIdField.interactable = value;
            if (_pinField != null) _pinField.interactable = value;
        }

        private void SetStatus(string text)
        {
            if (_status != null)
                _status.text = text;
            else
                Debug.Log("[token-entry] " + text);
        }

        private void OnDestroy()
        {
            if (_connectButton != null)
                _connectButton.onClick.RemoveListener(OnConnectClicked);
        }
    }
}
