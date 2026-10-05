package astroway

import "context"

// DrawChart draws a chart you already hold (the Data of Chart.Compute) for 1 credit instead
// of 10, because nothing is computed again. kind is "wheel" or "aspect-grid"; options is the
// render options object ({"theme": "css", "format": "svg", ...}) and may be nil.
// The image is byte for byte the one /render/wheel-western returns from birth data.
func (c *Client) DrawChart(ctx context.Context, chart any, kind string, options any, opts ...RequestOption) (*Response, error) {
	path := "/render/wheel-western"
	if kind == "aspect-grid" {
		path = "/render/aspect-grid"
	}
	return c.Post(ctx, path, drawBody{Chart: chart, Options: options}, opts...)
}

// DrawBiWheel draws two charts you already hold as a bi-wheel for 1 credit.
func (c *Client) DrawBiWheel(ctx context.Context, natal, outer any, options any, opts ...RequestOption) (*Response, error) {
	body := map[string]any{"natal": drawBody{Chart: natal}, "outer": drawBody{Chart: outer}}
	if options != nil {
		body["options"] = options
	}
	return c.Post(ctx, "/render/bi-wheel", body, opts...)
}

type drawBody struct {
	Chart   any `json:"chart"`
	Options any `json:"options,omitempty"`
}
