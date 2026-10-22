package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/elastic/go-elasticsearch/v8/esapi"
)

type FilmBounds struct {
	YearMin    int `json:"year_min"`
	YearMax    int `json:"year_max"`
	RuntimeMin int `json:"runtime_min"`
	RuntimeMax int `json:"runtime_max"`
}

type boundsResponse struct {
	Aggregations struct {
		YearMin    aggValue `json:"year_min"`
		YearMax    aggValue `json:"year_max"`
		RuntimeMin aggValue `json:"runtime_min"`
		RuntimeMax aggValue `json:"runtime_max"`
	} `json:"aggregations"`
}

type aggValue struct {
	Value *float64 `json:"value"`
}

func (c *Client) FilmBounds(ctx context.Context) (FilmBounds, error) {
	body := map[string]any{
		"size":  0,
		"query": map[string]any{"match_all": map[string]any{}},
		"aggs": map[string]any{
			"year_min":    map[string]any{"min": map[string]any{"field": "year"}},
			"year_max":    map[string]any{"max": map[string]any{"field": "year"}},
			"runtime_min": map[string]any{"min": map[string]any{"field": "runtime"}},
			"runtime_max": map[string]any{"max": map[string]any{"field": "runtime"}},
		},
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return FilmBounds{}, fmt.Errorf("search: сборка запроса границ: %w", err)
	}

	var resp boundsResponse
	req := esapi.SearchRequest{
		Index:        []string{c.FilmsIndex()},
		Body:         bytes.NewReader(payload),
		RequestCache: &cacheResults,
	}
	if err := c.read(req, ctx, &resp); err != nil {
		return FilmBounds{}, err
	}

	aggs := resp.Aggregations
	return FilmBounds{
		YearMin:    intValue(aggs.YearMin),
		YearMax:    intValue(aggs.YearMax),
		RuntimeMin: intValue(aggs.RuntimeMin),
		RuntimeMax: intValue(aggs.RuntimeMax),
	}, nil
}

func intValue(v aggValue) int {
	if v.Value == nil {
		return 0
	}
	return int(*v.Value)
}
