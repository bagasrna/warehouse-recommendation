package httpapi

import (
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"example.com/simpul-warehouse-selection/internal/assets"
	"example.com/simpul-warehouse-selection/internal/domain"
	"example.com/simpul-warehouse-selection/internal/insights"
	"example.com/simpul-warehouse-selection/internal/selection"
)

func handler(t *testing.T) http.Handler {
	t.Helper()
	c, err := assets.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	e, err := selection.New(c, 20)
	if err != nil {
		t.Fatal(err)
	}
	frontend, err := assets.Web()
	if err != nil {
		t.Fatal(err)
	}
	history, err := assets.HistoryCSV()
	if err != nil {
		t.Fatal(err)
	}
	historyData, err := insights.ParseCSV(history)
	if err != nil {
		t.Fatal(err)
	}
	return New(e, c, historyData.Report(), historyData, frontend, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()
}
func apiRequest(h http.Handler, method, path, body, contentType string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	return res
}
func TestEndpoints(t *testing.T) {
	h := handler(t)
	for _, path := range []string{"/", "/style.css", "/app.js", "/favicon.svg", "/api/v1/catalog", "/api/v1/insights", "/healthz"} {
		res := apiRequest(h, "GET", path, "", "")
		if res.Code != http.StatusOK {
			t.Fatalf("GET %s -> %d", path, res.Code)
		}
	}
	res := apiRequest(h, "POST", "/api/v1/recommendations", `{"items":[{"product_id":"P002","quantity":45}]}`, "application/json")
	if res.Code != 200 {
		t.Fatalf("got %d %s", res.Code, res.Body.String())
	}
	var body domain.Result
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "FULFILLABLE" || body.Recommendation.ScoreKMEq != 95 {
		t.Fatalf("unexpected response %+v", body)
	}
	res = apiRequest(h, "POST", "/api/v1/recommendations", `{"items":[{"product_id":"P002","quantity":109}]}`, "application/json")
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if res.Code != 200 || body.Status != "INSUFFICIENT_ELIGIBLE_STOCK" || body.Recommendation != nil {
		t.Fatalf("unexpected insufficient: %d %+v", res.Code, body)
	}
}
func TestHTTPValidation(t *testing.T) {
	h := handler(t)
	cases := []struct {
		body, ct string
		status   int
	}{
		{`{"items":[]}`, "application/json", 422},
		{`{"items":[{"product_id":"P001","quantity":0}]}`, "application/json", 422},
		{`{"items":[{"product_id":"P001","quantity":1.5}]}`, "application/json", 400},
		{`{"items":[{"product_id":"P001","quantity":"4"}]}`, "application/json", 400},
		{`{"items":[{"product_id":"UNKNOWN","quantity":4}]}`, "application/json", 422},
		{`{"items":[{"product_id":"P001","quantity":4,"extra":1}]}`, "application/json", 400},
		{`{"items":[{"product_id":"P001","quantity":4}]} {}`, "application/json", 400},
		{`invalid-json`, "application/json", 400},
		{`{}`, "text/plain", 415},
		{`{"items":[{"product_id":"P001","quantity":4}]}`, "application/json; charset=utf-8", 200},
		{`{"items":[],"padding":"` + strings.Repeat("x", 70*1024) + `"}`, "application/json", 413},
	}
	for _, tc := range cases {
		res := apiRequest(h, "POST", "/api/v1/recommendations", tc.body, tc.ct)
		if res.Code != tc.status {
			t.Errorf("%q: got %d, want %d. body: %s", tc.body[:min(30, len(tc.body))], res.Code, tc.status, res.Body.String())
		}
	}
	res := apiRequest(h, "GET", "/api/v1/recommendations", "", "")
	if res.Code != 405 {
		t.Errorf("expected 405, got %d", res.Code)
	}
}
func TestWebIsEmbedded(t *testing.T) {
	web, err := assets.Web()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"index.html", "style.css", "app.js", "favicon.svg"} {
		f, err := fs.ReadFile(web, name)
		if err != nil || len(f) == 0 {
			t.Errorf("missing asset %s", name)
		}
	}
}

func TestScheduleOptionalAndTimeAwareContext(t *testing.T) {
	h := handler(t)
	withSchedule := `{"items":[{"product_id":"P001","quantity":250}],"delivery_schedule":{"day":"Mon","time":"08:00"}}`
	res := apiRequest(h, "POST", "/api/v1/recommendations", withSchedule, "application/json")
	if res.Code != 200 {
		t.Fatalf("bad response %d %s", res.Code, res.Body.String())
	}
	var result domain.Result
	if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Recommendation == nil || result.Recommendation.History == nil || result.Recommendation.History.Period != "weekday_peak" {
		t.Fatalf("schedule not parsed: %+v", result)
	}
	if result.Recommendation.ScoreKMEq != 25 || result.Recommendation.History.IndicativeOrderMinutes != 60 {
		t.Fatalf("invalid history metrics: %+v", result.Recommendation)
	}
	core := apiRequest(h, "POST", "/api/v1/recommendations", `{"items":[{"product_id":"P001","quantity":250}]}`, "application/json")
	var baseline domain.Result
	if err := json.Unmarshal(core.Body.Bytes(), &baseline); err != nil {
		t.Fatal(err)
	}
	if baseline.Recommendation.History != nil || baseline.Recommendation.ScoreKMEq != result.Recommendation.ScoreKMEq {
		t.Fatal("schedule omitted must remain core only")
	}
	for _, schedule := range []string{
		`{"day":"Foo","time":"08:00"}`,
		`{"day":"Mon","time":"08:78"}`,
		`{"day":"Mon"}`,
	} {
		res = apiRequest(h, "POST", "/api/v1/recommendations", `{"items":[{"product_id":"P001","quantity":250}],"delivery_schedule":`+schedule+`}`, "application/json")
		if res.Code != 422 || !strings.Contains(res.Body.String(), "delivery_schedule") {
			t.Fatalf("bad validation: %d %s", res.Code, res.Body.String())
		}
	}
	res = apiRequest(h, "POST", "/api/v1/recommendations", `{"items":[{"product_id":"P001","quantity":250}],"delivery_schedule":{"day":"Mon","time":"08:00","unknown":true}}`, "application/json")
	if res.Code != 400 {
		t.Fatalf("expected strict JSON, got %d", res.Code)
	}
}
