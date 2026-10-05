//go:build live

package astroway

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

// go test -tags live ./... with ASTROWAY_API_KEY set; spends about 31 credits.
func TestLiveChartAndDraw(t *testing.T) {
	key := os.Getenv("ASTROWAY_API_KEY")
	if key == "" {
		t.Skip("ASTROWAY_API_KEY not set")
	}
	aw := New(key)
	ctx := context.Background()
	birth := map[string]any{"date": "1990-07-14", "time": "14:30:00", "timezoneOffset": 3, "latitude": 50.4501, "longitude": 30.5234, "houseSystem": "P"}

	res, err := aw.Chart.Compute(ctx, birth)
	if err != nil {
		t.Fatal(err)
	}
	var chart struct {
		Houses struct{ Ascendant float64 } `json:"houses"`
	}
	if err := res.Decode(&chart); err != nil || int(chart.Houses.Ascendant) != 212 {
		t.Fatalf("ascendant %v %v", chart.Houses.Ascendant, err)
	}

	opts := map[string]any{"theme": "css"}
	drawn, err := aw.DrawChart(ctx, res.Data, "wheel", opts)
	if err != nil {
		t.Fatal(err)
	}
	fromBirth, err := aw.Render.WheelWestern(ctx, map[string]any{"date": "1990-07-14", "time": "14:30:00", "timezoneOffset": 3,
		"latitude": 50.4501, "longitude": 30.5234, "houseSystem": "P", "options": opts})
	if err != nil {
		t.Fatal(err)
	}
	var a, b struct {
		SVG string `json:"svg"`
	}
	_ = json.Unmarshal(drawn.Data, &a)
	_ = json.Unmarshal(fromBirth.Data, &b)
	if a.SVG == "" || a.SVG != b.SVG {
		t.Fatalf("drawn chart differs from birth render (%d vs %d bytes)", len(a.SVG), len(b.SVG))
	}
	t.Logf("svg %d bytes, credits left %v", len(a.SVG), *drawn.CreditsRemaining)
}
