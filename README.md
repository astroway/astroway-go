# astroway-go

Official Go client for the [AstroWay API](https://api.astroway.info): natal charts, synastry, transits, Vedic dashas, Human Design, Tarot, numerology, SVG chart wheels and 700+ more endpoints.

[![Go Reference](https://pkg.go.dev/badge/github.com/astroway/astroway-go.svg)](https://pkg.go.dev/github.com/astroway/astroway-go)
[![license: MIT](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

Standard library only. `context.Context` on every call, retries on 408/409/429/5xx with jittered backoff and `Retry-After`, one Idempotency-Key per POST kept across retries, typed errors you test with `errors.Is`.

## Install

```bash
go get github.com/astroway/astroway-go
```

Requires Go 1.22+. Get a key at <https://api.astroway.info/dashboard/sign-up>: 10 000 credits a month free, no card. Each endpoint costs 1 to 500 credits depending on what it computes ([pricing](https://api.astroway.info/pricing/)).

## Quick start

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	astroway "github.com/astroway/astroway-go"
)

func main() {
	aw := astroway.New(os.Getenv("ASTROWAY_API_KEY"))

	res, err := aw.Chart.Compute(context.Background(), map[string]any{
		"date": "1990-07-14", "time": "14:30:00", "timezoneOffset": 3,
		"latitude": 50.4501, "longitude": 30.5234, "houseSystem": "P",
	})
	if err != nil {
		log.Fatal(err)
	}

	var chart struct {
		Houses struct {
			Ascendant float64 `json:"ascendant"`
		} `json:"houses"`
	}
	if err := res.Decode(&chart); err != nil {
		log.Fatal(err)
	}
	fmt.Println(chart.Houses.Ascendant) // 212.0952574979425
}
```

`/chart` returns positions, not labels: the ascendant and every planet's `longitude` are ecliptic longitudes in degrees, so the sign is `longitude / 30` into Aries..Pisces. A complete program that also prints the sign and saves the wheel is in [`examples/quickstart`](examples/quickstart/main.go).

## How calls look

Every endpoint is a method on a namespace field, named after the API path:

| Endpoint | Method |
|---|---|
| `POST /chart` | `aw.Chart.Compute(ctx, body)` |
| `POST /synastry` | `aw.Synastry.Compute(ctx, body)` |
| `POST /vedic/dashas/vimshottari/maha` | `aw.Vedic.DashasVimshottariMaha(ctx, body)` |
| `POST /render/wheel-western` | `aw.Render.WheelWestern(ctx, body)` |
| `GET /health` | `aw.Health.Compute(ctx)` |

The body is anything `encoding/json` can marshal: a `map[string]any`, your own struct with JSON tags, or `json.RawMessage`. The `{ ok, data, error }` envelope is unwrapped, so `res.Data` is the `data` member and `res.Decode(&v)` fills your own type. `res.RequestID` and `res.CreditsRemaining` come from the response headers.

Anything without a generated method is one call away:

```go
res, err := aw.Do(ctx, "POST", "/webhooks/wh_123/test", nil)
```

### Query parameters

```go
aw.Chart.Compute(ctx, body,
	astroway.WithQuery("lang", "uk"),          // response language, where the endpoint has one
	astroway.WithQuery("fields", "planets"),   // only these top-level fields
	astroway.WithQuery("precision", "2"))      // round numbers to 2 decimals
```

### Draw a chart you already have, for 1 credit

`/render/*` computes the chart from birth data for 10 credits. When you already hold the chart from `Chart.Compute`, send it as is and only the drawing is charged:

```go
img, err := aw.DrawChart(ctx, res.Data, "wheel", map[string]any{"theme": "css"})        // or "aspect-grid"
img, err = aw.DrawBiWheel(ctx, natal.Data, partner.Data, map[string]any{"crossAspects": true})
```

The SVG is byte for byte the one you would get from birth data. `theme: "css"` paints every colour as a CSS variable with stable `aw-*` classes, see [SVG charts](https://api.astroway.info/en/products/svg-charts/).

## Errors

```go
_, err := aw.Chart.Compute(ctx, body)
switch {
case errors.Is(err, astroway.ErrQuotaExceeded): // out of credits; retrying will not help
case errors.Is(err, astroway.ErrRateLimit):     // still 429 after the retries
case errors.Is(err, astroway.ErrBadRequest):    // the body failed validation
}

var apiErr *astroway.APIError
if errors.As(err, &apiErr) {
	log.Printf("%d %s %s request_id=%s", apiErr.StatusCode, apiErr.Code, apiErr.Message, apiErr.RequestID)
}
```

Kinds: `ErrBadRequest`, `ErrAuthentication`, `ErrPermissionDenied`, `ErrNotFound`, `ErrUnprocessableEntity`, `ErrRateLimit`, `ErrQuotaExceeded`, `ErrCalculation`, `ErrInternalServer`. A network failure after all retries is a `*ConnectionError`. Quote the request id when you write to support.

## Options

```go
aw := astroway.New(key,
	astroway.WithMaxRetries(4),                                   // default 2, 0 disables
	astroway.WithHTTPClient(&http.Client{Timeout: 10 * time.Second}), // default timeout 30 s
	astroway.WithBearerAuth(),                                    // Authorization: Bearer instead of X-Api-Key
	astroway.WithBaseURL("http://localhost:3101/v1"))
```

Per call: `WithQuery`, `WithHeader`, `WithIdempotencyKey`.

## Links

- API reference: <https://api.astroway.info/docs/api/>
- Other SDKs: <https://api.astroway.info/sdk/>
- Changelog: <https://api.astroway.info/en/changelog/>

MIT licence.
