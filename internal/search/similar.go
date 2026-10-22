package search

import (
	"context"
	"strings"
)

func (c *Client) SimilarTo(ctx context.Context, filmID uint, limit int) (Page[FilmDoc], error) {
	if limit <= 0 || limit > 50 {
		limit = 12
	}

	source, err := c.filmByID(ctx, filmID)
	if err != nil {
		return Page[FilmDoc]{}, err
	}
	if source == nil {
		return Page[FilmDoc]{Items: []FilmDoc{}}, nil
	}

	should := make([]any, 0, 16)

	appendNames(&should, "directors.raw", source.Directors, 10)
	appendNames(&should, "cast.raw", source.Cast, 2)
	for _, genre := range source.Genres {
		should = append(should, map[string]any{
			"term": map[string]any{"genres": map[string]any{"value": genre, "boost": 2}},
		})
	}
	if source.Synopsis != "" {
		should = append(should, map[string]any{
			"match": map[string]any{
				"synopsis": map[string]any{"query": source.Synopsis, "boost": 0.5},
			},
		})
	}
	if len(should) == 0 {
		return Page[FilmDoc]{Items: []FilmDoc{}}, nil
	}

	boolQuery := map[string]any{
		"should":               should,
		"minimum_should_match": 1,

		"must_not": map[string]any{"term": map[string]any{"id": filmID}},
	}

	if len(source.Genres) > 0 {
		boolQuery["filter"] = []any{
			map[string]any{"terms": map[string]any{"genres": source.Genres}},
		}
	}

	body := map[string]any{
		"size":  limit,
		"query": map[string]any{"bool": boolQuery},
		"sort": []any{
			map[string]any{"_score": map[string]any{"order": "desc"}},
			map[string]any{"popularity": map[string]any{"order": "desc"}},
			map[string]any{"id": map[string]any{"order": "desc"}},
		},
		"_source": listSource(),
	}

	return runSearch[FilmDoc](ctx, c, c.FilmsIndex(), body, limit, searchOptions{})
}

func (c *Client) filmByID(ctx context.Context, filmID uint) (*FilmDoc, error) {
	body := map[string]any{
		"size":  1,
		"query": map[string]any{"term": map[string]any{"id": filmID}},
	}
	page, err := runSearch[FilmDoc](ctx, c, c.FilmsIndex(), body, 1, searchOptions{})
	if err != nil {
		return nil, err
	}
	if len(page.Items) == 0 {
		return nil, nil
	}
	return &page.Items[0], nil
}

func appendNames(clauses *[]any, field string, names []string, boost float64) {
	for _, name := range names {
		value := strings.ToLower(strings.TrimSpace(name))
		if value == "" {
			continue
		}
		*clauses = append(*clauses, map[string]any{
			"term": map[string]any{
				field: map[string]any{"value": value, "boost": boost},
			},
		})
	}
}

type TasteProfile struct {
	Genres    map[string]int
	Directors map[string]int
	Cast      map[string]int

	Kind string

	ExcludeIDs []uint
}

func (p TasteProfile) Empty() bool {
	return len(p.Genres) == 0 && len(p.Directors) == 0 && len(p.Cast) == 0
}

func (c *Client) RecommendFor(ctx context.Context, profile TasteProfile, limit int) (Page[FilmDoc], error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	if profile.Empty() {
		return Page[FilmDoc]{Items: []FilmDoc{}}, nil
	}
	return runSearch[FilmDoc](ctx, c, c.FilmsIndex(), recommendBody(profile, limit), limit, searchOptions{})
}

func recommendBody(profile TasteProfile, limit int) map[string]any {
	should := make([]any, 0, 16)

	appendTerms(&should, "genres", profile.Genres, 1)
	appendTerms(&should, "directors.raw", profile.Directors, 6)
	appendTerms(&should, "cast.raw", profile.Cast, 3)

	boolQuery := map[string]any{
		"should":               should,
		"minimum_should_match": 1,
	}
	if profile.Kind != "" {
		boolQuery["filter"] = []any{
			map[string]any{"term": map[string]any{"kind": profile.Kind}},
		}
	}
	if excluded := excludeClause(profile.ExcludeIDs); excluded != nil {
		boolQuery["must_not"] = excluded
	}

	return map[string]any{
		"size": limit,
		"query": map[string]any{
			"function_score": map[string]any{
				"query": map[string]any{"bool": boolQuery},
				"field_value_factor": map[string]any{
					"field":    "popularity",
					"modifier": "log1p",
					"factor":   0.6,
					"missing":  0,
				},
				"boost_mode": "multiply",
			},
		},
		"sort": []any{
			map[string]any{"_score": map[string]any{"order": "desc"}},
			map[string]any{"id": map[string]any{"order": "desc"}},
		},
		"_source": listSource(),
	}
}

func appendTerms(clauses *[]any, field string, weights map[string]int, base float64) {
	for value, count := range weights {
		*clauses = append(*clauses, map[string]any{
			"term": map[string]any{
				field: map[string]any{"value": value, "boost": base * float64(count)},
			},
		})
	}
}
