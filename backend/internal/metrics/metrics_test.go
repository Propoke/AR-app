package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMiddlewareRecordsRequests(t *testing.T) {
	h := Middleware("/test/route", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})

	req := httptest.NewRequest(http.MethodGet, "/test/route", nil)
	h(httptest.NewRecorder(), req)

	// Scrape /metrics and confirm the request was counted with our labels.
	scrape := httptest.NewRecorder()
	Handler().ServeHTTP(scrape, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := scrape.Body.String()

	if !strings.Contains(body, `ar_http_requests_total{method="GET",route="/test/route",status="418"}`) {
		t.Fatalf("expected request counter for the route/status; got:\n%s", firstLines(body, 40))
	}
	if !strings.Contains(body, "ar_http_request_duration_seconds") {
		t.Fatal("expected duration histogram to be present")
	}
}

func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}
