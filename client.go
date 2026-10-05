// Package astroway is the official Go client for the AstroWay API (https://api.astroway.info).
//
//	aw := astroway.New(os.Getenv("ASTROWAY_API_KEY"))
//	res, err := aw.Chart.Compute(ctx, map[string]any{"date": "1990-07-14", "time": "14:30:00",
//		"timezoneOffset": 3, "latitude": 50.45, "longitude": 30.52})
//
// Every endpoint of the spec is a method on a namespace field of Client. Bodies are anything
// encoding/json can marshal; results come back as raw JSON with the { ok, data, error }
// envelope already unwrapped, ready for Response.Decode into your own type.
package astroway

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	mrand "math/rand/v2"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Version is the SDK version, sent in the User-Agent.
const Version = "0.1.0"

// DefaultBaseURL is the production API root.
const DefaultBaseURL = "https://api.astroway.info/v1"

// Client talks to the AstroWay API. It is safe for concurrent use.
type Client struct {
	apiKey      string
	bearer      bool
	baseURL     string
	http        *http.Client
	maxRetries  int
	baseDelay   time.Duration
	maxDelay    time.Duration
	idempotency bool
	userAgent   string

	namespaces
}

// Option configures a Client.
type Option func(*Client)

// WithBaseURL points the client at another API root, such as a local server.
func WithBaseURL(u string) Option { return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") } }

// WithHTTPClient replaces the underlying *http.Client (timeouts, proxies, transports).
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// WithMaxRetries sets how many times a retryable failure is retried. 0 disables retries.
func WithMaxRetries(n int) Option { return func(c *Client) { c.maxRetries = n } }

// WithBearerAuth sends the key as "Authorization: Bearer" instead of "X-Api-Key".
func WithBearerAuth() Option { return func(c *Client) { c.bearer = true } }

// WithoutIdempotency stops the client attaching an Idempotency-Key to POST requests.
func WithoutIdempotency() Option { return func(c *Client) { c.idempotency = false } }

// New returns a client for the given API key.
func New(apiKey string, opts ...Option) *Client {
	c := &Client{
		apiKey:      apiKey,
		baseURL:     DefaultBaseURL,
		http:        &http.Client{Timeout: 30 * time.Second},
		maxRetries:  2,
		baseDelay:   250 * time.Millisecond,
		maxDelay:    30 * time.Second,
		idempotency: true,
		userAgent:   fmt.Sprintf("astroway-sdk-go/%s (%s; %s-%s)", Version, runtime.Version(), runtime.GOOS, runtime.GOARCH),
	}
	for _, o := range opts {
		o(c)
	}
	c.namespaces.init(c)
	return c
}

// Response is a successful API answer.
type Response struct {
	// Data is the "data" member of the envelope, or the whole body when there is no envelope.
	Data             json.RawMessage
	StatusCode       int
	Header           http.Header
	RequestID        string
	CreditsRemaining *int
}

// Decode unmarshals Data into v.
func (r *Response) Decode(v any) error { return json.Unmarshal(r.Data, v) }

// RequestOption adjusts one call.
type RequestOption func(*requestConfig)

type requestConfig struct {
	query          url.Values
	header         http.Header
	idempotencyKey string
}

// WithQuery adds a query parameter, e.g. WithQuery("lang", "uk"), WithQuery("fields", "planets").
func WithQuery(key, value string) RequestOption {
	return func(r *requestConfig) { r.query.Add(key, value) }
}

// WithHeader adds a request header.
func WithHeader(key, value string) RequestOption {
	return func(r *requestConfig) { r.header.Add(key, value) }
}

// WithIdempotencyKey sets the Idempotency-Key, so a retried POST is not charged twice.
func WithIdempotencyKey(key string) RequestOption {
	return func(r *requestConfig) { r.idempotencyKey = key }
}

var retryableStatus = map[int]bool{408: true, 409: true, 429: true, 500: true, 502: true, 503: true, 504: true}

// Do sends one request. path is relative to the base URL ("/chart"); body may be nil.
func (c *Client) Do(ctx context.Context, method, path string, body any, opts ...RequestOption) (*Response, error) {
	rc := requestConfig{query: url.Values{}, header: http.Header{}}
	for _, o := range opts {
		o(&rc)
	}
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return nil, fmt.Errorf("astroway: encode body: %w", err)
		}
	}
	target := c.baseURL + path
	if len(rc.query) > 0 {
		target += "?" + rc.query.Encode()
	}
	idem := rc.idempotencyKey
	if idem == "" && c.idempotency && method == http.MethodPost && rc.header.Get("Idempotency-Key") == "" {
		idem = newIdempotencyKey()
	}

	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(payload))
		if err != nil {
			return nil, fmt.Errorf("astroway: build request: %w", err)
		}
		for k, vs := range rc.header {
			for _, v := range vs {
				req.Header.Add(k, v)
			}
		}
		req.Header.Set("User-Agent", c.userAgent)
		req.Header.Set("X-Astroway-Channel", "sdk-go")
		req.Header.Set("Accept", "application/json")
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if c.bearer {
			req.Header.Set("Authorization", "Bearer "+c.apiKey)
		} else {
			req.Header.Set("X-Api-Key", c.apiKey)
		}
		if idem != "" {
			req.Header.Set("Idempotency-Key", idem)
		}

		resp, err := c.http.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if attempt < c.maxRetries {
				if werr := sleep(ctx, c.backoff(attempt, nil)); werr != nil {
					return nil, werr
				}
				continue
			}
			return nil, &ConnectionError{Path: path, Err: err}
		}
		raw, rerr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if rerr != nil {
			return nil, &ConnectionError{Path: path, Err: rerr}
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return &Response{
				Data:             unwrap(raw),
				StatusCode:       resp.StatusCode,
				Header:           resp.Header,
				RequestID:        resp.Header.Get("X-Request-Id"),
				CreditsRemaining: intHeader(resp.Header.Get("X-Credits-Remaining")),
			}, nil
		}
		apiErr := newAPIError(resp, raw)
		if retryableStatus[resp.StatusCode] && attempt < c.maxRetries && !errors.Is(apiErr, ErrQuotaExceeded) {
			if werr := sleep(ctx, c.backoff(attempt, apiErr.RetryAfter)); werr != nil {
				return nil, werr
			}
			continue
		}
		return nil, apiErr
	}
}

// Get is Do with GET and no body.
func (c *Client) Get(ctx context.Context, path string, opts ...RequestOption) (*Response, error) {
	return c.Do(ctx, http.MethodGet, path, nil, opts...)
}

// Post is Do with POST.
func (c *Client) Post(ctx context.Context, path string, body any, opts ...RequestOption) (*Response, error) {
	return c.Do(ctx, http.MethodPost, path, body, opts...)
}

// Full-jitter exponential backoff; Retry-After wins when the server sends it.
func (c *Client) backoff(attempt int, retryAfter *time.Duration) time.Duration {
	if retryAfter != nil {
		return min(*retryAfter, c.maxDelay)
	}
	upper := math.Min(float64(c.maxDelay), float64(c.baseDelay)*math.Pow(2, float64(attempt)))
	return time.Duration(mrand.Float64() * upper)
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func unwrap(raw []byte) json.RawMessage {
	var env map[string]json.RawMessage
	if json.Unmarshal(raw, &env) == nil {
		if d, ok := env["data"]; ok {
			return d
		}
	}
	return raw
}

func intHeader(v string) *int {
	if v == "" {
		return nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return nil
	}
	return &n
}

func newIdempotencyKey() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
