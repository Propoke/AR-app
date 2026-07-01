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

// TestMiddlewareDynamicResolvesRouteAfterNext confirms routeFn is called AFTER
// next has served the request, not before. This is essential for callers like
// http.ServeMux, which only populate the matched-pattern info on the request
// as a side effect of its own ServeHTTP — a routeFn evaluated up front (before
// next runs) would see stale/empty state.
func TestMiddlewareDynamicResolvesRouteAfterNext(t *testing.T) {
	var routeFnCalledAfterHandler bool
	handlerRan := false

	routeFn := func(r *http.Request) string {
		routeFnCalledAfterHandler = handlerRan
		return "dynamic-route"
	}
	h := MiddlewareDynamic(routeFn, func(w http.ResponseWriter, r *http.Request) {
		handlerRan = true
		w.WriteHeader(http.StatusOK)
	})

	h(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))

	if !routeFnCalledAfterHandler {
		t.Fatal("routeFn must be called after next has served the request")
	}

	scrape := httptest.NewRecorder()
	Handler().ServeHTTP(scrape, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := scrape.Body.String()
	if !strings.Contains(body, `ar_http_requests_total{method="GET",route="dynamic-route",status="200"}`) {
		t.Fatalf("expected counter labeled with the dynamically resolved route; got:\n%s", firstLines(body, 40))
	}
}

// TestSignalingRouteExcludedFromDurationHistogram confirms the long-lived
// signaling WebSocket route is still counted (useful as a connections-opened
// counter) but excluded from the latency histogram, whose buckets would
// otherwise be dominated by connection lifetimes rather than request latency.
func TestSignalingRouteExcludedFromDurationHistogram(t *testing.T) {
	h := Middleware(signalingRoute, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/signaling", nil))

	scrape := httptest.NewRecorder()
	Handler().ServeHTTP(scrape, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := scrape.Body.String()

	wantCounter := `ar_http_requests_total{method="GET",route="` + signalingRoute + `",status="200"}`
	if !strings.Contains(body, wantCounter) {
		t.Fatalf("expected the signaling route to still be counted; got:\n%s", firstLines(body, 40))
	}
	wantHistogramSeries := `ar_http_request_duration_seconds_count{route="` + signalingRoute + `"}`
	if strings.Contains(body, wantHistogramSeries) {
		t.Fatalf("signaling route must be excluded from the duration histogram; got:\n%s", firstLines(body, 40))
	}
}

func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}
