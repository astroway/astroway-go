// Run with ASTROWAY_API_KEY set: go run ./examples/quickstart
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	astroway "github.com/astroway/astroway-go"
)

var signs = []string{"Aries", "Taurus", "Gemini", "Cancer", "Leo", "Virgo",
	"Libra", "Scorpio", "Sagittarius", "Capricorn", "Aquarius", "Pisces"}

type chart struct {
	Planets []struct {
		Name      string  `json:"name"`
		Longitude float64 `json:"longitude"`
	} `json:"planets"`
	Houses struct {
		Ascendant float64 `json:"ascendant"`
	} `json:"houses"`
}

func main() {
	aw := astroway.New(os.Getenv("ASTROWAY_API_KEY"))
	ctx := context.Background()

	res, err := aw.Chart.Compute(ctx, map[string]any{
		"date": "1990-07-14", "time": "14:30:00", "timezoneOffset": 3,
		"latitude": 50.4501, "longitude": 30.5234, "houseSystem": "P",
	})
	if errors.Is(err, astroway.ErrQuotaExceeded) {
		log.Fatal("out of credits: top up at https://api.astroway.info/pricing/")
	}
	if err != nil {
		log.Fatal(err)
	}
	var c chart
	if err := res.Decode(&c); err != nil {
		log.Fatal(err)
	}
	asc := c.Houses.Ascendant
	fmt.Printf("ASC: %s %.2f°\n", signs[int(asc/30)], asc-30*float64(int(asc/30)))
	fmt.Printf("%s: %.2f°\n", c.Planets[0].Name, c.Planets[0].Longitude)

	// The same chart as an SVG wheel, for 1 credit: nothing is computed again.
	img, err := aw.DrawChart(ctx, res.Data, "wheel", map[string]any{"theme": "css"})
	if err != nil {
		log.Fatal(err)
	}
	var out struct {
		SVG string `json:"svg"`
	}
	if err := img.Decode(&out); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile("wheel.svg", []byte(out.SVG), 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("wheel.svg: %d bytes\n", len(out.SVG))
}
