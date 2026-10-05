package astroway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func server(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	c := New("aw_test_key", WithBaseURL(s.URL))
	c.baseDelay = time.Millisecond
	return c
}

func TestUnwrapsEnvelopeAndReadsHeaders(t *testing.T) {
	c := server(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chart" || r.Method != "POST" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("X-Api-Key") != "aw_test_key" || r.Header.Get("X-Astroway-Channel") != "sdk-go" {
			t.Errorf("auth headers: %v", r.Header)
		}
		if r.URL.Query().Get("lang") != "uk" {
			t.Errorf("query lost: %s", r.URL.RawQuery)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["date"] != "1990-07-14" {
			t.Errorf("body: %v", body)
		}
		w.Header().Set("X-Request-Id", "req_1")
		w.Header().Set("X-Credits-Remaining", "9980")
		_, _ = io.WriteString(w, `{"ok":true,"data":{"houses":{"ascendant":212.09}}}`)
	})
	res, err := c.Chart.Compute(context.Background(), map[string]any{"date": "1990-07-14"}, WithQuery("lang", "uk"))
	if err != nil {
		t.Fatal(err)
	}
	var chart struct {
		Houses struct{ Ascendant float64 } `json:"houses"`
	}
	if err := res.Decode(&chart); err != nil || chart.Houses.Ascendant != 212.09 {
		t.Fatalf("decode: %v %+v", err, chart)
	}
	if res.RequestID != "req_1" || res.CreditsRemaining == nil || *res.CreditsRemaining != 9980 {
		t.Fatalf("metadata: %+v", res)
	}
}

func TestBodyWithoutEnvelopeIsReturnedWhole(t *testing.T) {
	c := server(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, `[1,2]`) })
	res, err := c.Get(context.Background(), "/anything")
	if err != nil || string(res.Data) != `[1,2]` {
		t.Fatalf("%v %s", err, res.Data)
	}
}

func TestClassifiesErrors(t *testing.T) {
	cases := []struct {
		status int
		code   string
		want   error
	}{
		{400, "VALIDATION_ERROR", ErrBadRequest},
		{401, "", ErrAuthentication},
		{402, "", ErrQuotaExceeded},
		{403, "", ErrPermissionDenied},
		{404, "", ErrNotFound},
		{422, "", ErrUnprocessableEntity},
		{429, "OUT_OF_CREDITS", ErrQuotaExceeded},
		{400, "CALCULATION_ERROR", ErrCalculation},
		{418, "", ErrUnexpectedReply},
	}
	for _, tc := range cases {
		c := server(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Request-Id", "req_e")
			w.WriteHeader(tc.status)
			_, _ = io.WriteString(w, `{"ok":false,"error":{"code":"`+tc.code+`","message":"nope"}}`)
		})
		_, err := c.Post(context.Background(), "/chart", map[string]any{})
		var apiErr *APIError
		if !errors.Is(err, tc.want) || !errors.As(err, &apiErr) {
			t.Fatalf("%d %s: got %v", tc.status, tc.code, err)
		}
		if apiErr.Message != "nope" || apiErr.RequestID != "req_e" || apiErr.StatusCode != tc.status {
			t.Fatalf("fields: %+v", apiErr)
		}
	}
}

func TestRetriesKeepTheSameIdempotencyKey(t *testing.T) {
	var calls atomic.Int32
	keys := make(chan string, 3)
	c := server(t, func(w http.ResponseWriter, r *http.Request) {
		keys <- r.Header.Get("Idempotency-Key")
		if calls.Add(1) < 3 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(503)
			return
		}
		_, _ = io.WriteString(w, `{"ok":true,"data":1}`)
	})
	if _, err := c.Post(context.Background(), "/chart", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	a, b, d := <-keys, <-keys, <-keys
	if a == "" || a != b || b != d {
		t.Fatalf("keys differ across retries: %q %q %q", a, b, d)
	}
}

func TestGivesUpAfterMaxRetries(t *testing.T) {
	var calls atomic.Int32
	c := server(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) })
	_, err := c.Post(context.Background(), "/chart", nil)
	if !errors.Is(err, ErrInternalServer) || calls.Load() != 3 {
		t.Fatalf("err %v after %d calls", err, calls.Load())
	}
}

func TestQuotaIsNotRetried(t *testing.T) {
	var calls atomic.Int32
	c := server(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(429)
		_, _ = io.WriteString(w, `{"ok":false,"error":{"code":"OUT_OF_CREDITS","message":"top up"}}`)
	})
	_, err := c.Post(context.Background(), "/chart", nil)
	if !errors.Is(err, ErrQuotaExceeded) || calls.Load() != 1 {
		t.Fatalf("err %v after %d calls", err, calls.Load())
	}
}

func TestGetSendsNoIdempotencyKeyAndBearerWorks(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Idempotency-Key") != "" || r.Header.Get("Authorization") != "Bearer k" || r.Header.Get("X-Api-Key") != "" {
			t.Errorf("headers: %v", r.Header)
		}
		_, _ = io.WriteString(w, `{"ok":true,"data":{}}`)
	}))
	defer s.Close()
	if _, err := New("k", WithBaseURL(s.URL), WithBearerAuth()).Health.Compute(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestDrawChartSendsTheChartAsIs(t *testing.T) {
	c := server(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch r.URL.Path {
		case "/render/aspect-grid":
			if string(body["chart"]) != `{"planets":[]}` || string(body["options"]) != `{"theme":"css"}` {
				t.Errorf("body: %s %s", body["chart"], body["options"])
			}
		case "/render/bi-wheel":
			if string(body["natal"]) != `{"chart":{"a":1}}` || string(body["outer"]) != `{"chart":{"b":2}}` {
				t.Errorf("bi body: %s %s", body["natal"], body["outer"])
			}
		default:
			t.Errorf("path %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"ok":true,"data":{"svg":"<svg/>"}}`)
	})
	ctx := context.Background()
	if _, err := c.DrawChart(ctx, json.RawMessage(`{"planets":[]}`), "aspect-grid", map[string]any{"theme": "css"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.DrawBiWheel(ctx, map[string]int{"a": 1}, map[string]int{"b": 2}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestContextCancelStopsRetrying(t *testing.T) {
	c := server(t, func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Retry-After", "10"); w.WriteHeader(503) })
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.Post(ctx, "/chart", nil)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 2*time.Second {
		t.Fatalf("err %v after %v", err, time.Since(start))
	}
}
