package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/elastic/go-elasticsearch/v8/esapi"
)

type SortMode string

const (
	SortRelevance  SortMode = "relevance"
	SortPopularity SortMode = "popularity"
	SortTMDBRating SortMode = "tmdb_rating"
	SortLocal      SortMode = "local_rating"
	SortYear       SortMode = "year"
	SortRuntime    SortMode = "runtime"
	SortTitle      SortMode = "title"
)

const SortRating SortMode = "rating"

type SortOrder string

const (
	OrderDesc SortOrder = "desc"
	OrderAsc  SortOrder = "asc"
)

type Scope string

const (
	ScopeAll   Scope = "all"
	ScopeTitle Scope = "title"
	ScopeCast  Scope = "cast"
)

func (s Scope) Valid() bool {
	return s == "" || s == ScopeAll || s == ScopeTitle || s == ScopeCast
}

var sortFields = map[SortMode]string{
	SortPopularity: "popularity",
	SortTMDBRating: "tmdb_rating",
	SortRating:     "tmdb_rating",
	SortLocal:      "local_rating",
	SortYear:       "year",
	SortRuntime:    "runtime",
	SortTitle:      "title_sort",
}

func (m SortMode) Valid() bool {
	if m == SortRelevance {
		return true
	}
	_, ok := sortFields[m]
	return ok
}

func (o SortOrder) Valid() bool {
	return o == OrderDesc || o == OrderAsc
}

type Filters struct {
	Kind        string
	Genres      []string
	YearFrom    int
	YearTo      int
	RuntimeFrom int
	RuntimeTo   int
	RatingFrom  float64
	LocalFrom   float64

	ExcludeIDs []uint
}

func (f Filters) empty() bool {
	return f.Kind == "" && len(f.Genres) == 0 && f.YearFrom == 0 && f.YearTo == 0 &&
		f.RuntimeFrom == 0 && f.RuntimeTo == 0 && f.RatingFrom == 0 && f.LocalFrom == 0
}

type Query struct {
	Text    string
	Scope   Scope
	Filters Filters
	Sort    SortMode

	Order SortOrder
	Limit int

	After []any

	WithFacets bool

	Lean bool

	SkipTotal bool
}

type Page[T any] struct {
	Items     []T
	Total     int
	NextAfter []any
	Facets    []FacetBucket
}

type FacetBucket struct {
	Key   string
	Count int
}

func (q Query) limit() int {
	if q.Limit <= 0 || q.Limit > 50 {
		return 20
	}
	return q.Limit
}

func (c *Client) SearchFilms(ctx context.Context, q Query) (Page[FilmDoc], error) {
	limit := q.limit()
	return runSearch[FilmDoc](ctx, c, c.FilmsIndex(), filmSearchBody(q), limit, searchOptions{
		exactTotal: !q.SkipTotal,
	})
}

func filmSearchBody(q Query) map[string]any {
	body := map[string]any{
		"size":    q.limit(),
		"query":   filmQuery(q),
		"sort":    filmSort(q),
		"_source": listSource(),
	}
	if q.Lean {
		body["_source"] = gridSource()
	}
	if len(q.After) > 0 {
		body["search_after"] = q.After
	}
	if q.WithFacets {
		body["aggs"] = map[string]any{
			"genres": map[string]any{
				"terms": map[string]any{"field": "genres", "size": 30},
			},
		}
	}
	return body
}

func listSource() map[string]any {
	return map[string]any{
		"excludes": []string{
			"synopsis", "directors_original", "cast_original", "genre_names",
			"cast", "tmdb_votes", "genres",
		},
	}
}

func gridSource() map[string]any {
	return map[string]any{
		"includes": []string{
			"id", "title_ru", "title_en", "kind", "year", "poster_url",
			"tmdb_rating", "local_rating", "local_votes",
		},
	}
}

func filmQuery(q Query) map[string]any {
	filters := buildFilmFilters(q.Filters)
	excluded := excludeClause(q.Filters.ExcludeIDs)

	if q.Text == "" {
		if len(filters) == 0 && excluded == nil {
			return map[string]any{"match_all": map[string]any{}}
		}
		boolQuery := map[string]any{}
		if len(filters) > 0 {
			boolQuery["filter"] = filters
		}
		if excluded != nil {
			boolQuery["must_not"] = excluded
		}
		return map[string]any{"bool": boolQuery}
	}

	boolQuery := map[string]any{
		"should":               filmClauses(q.Text, q.Scope),
		"minimum_should_match": 1,
	}
	if len(filters) > 0 {
		boolQuery["filter"] = filters
	}
	if excluded != nil {
		boolQuery["must_not"] = excluded
	}

	return map[string]any{"bool": boolQuery}
}

func excludeClause(ids []uint) map[string]any {
	if len(ids) == 0 {
		return nil
	}
	values := make([]uint, len(ids))
	copy(values, ids)
	return map[string]any{"terms": map[string]any{"id": values}}
}

func filmClauses(text string, scope Scope) []any {
	if scope == "" {
		scope = ScopeAll
	}
	clauses := make([]any, 0, 7)

	if scope != ScopeCast {
		clauses = append(clauses,

			map[string]any{
				"multi_match": map[string]any{
					"query":  strings.ToLower(text),
					"fields": []string{"title_ru.raw^100", "title_en.raw^80"},
				},
			},

			map[string]any{
				"multi_match": map[string]any{
					"query":  text,
					"fields": []string{"title_ru^10", "title_en^8"},
					"type":   "phrase",
				},
			},

			map[string]any{
				"multi_match": map[string]any{
					"query":         text,
					"fields":        []string{"title_ru^2", "title_en^1.5"},
					"type":          "best_fields",
					"fuzziness":     "AUTO",
					"prefix_length": 2,
				},
			},

			map[string]any{
				"multi_match": map[string]any{
					"query":  text,
					"fields": []string{"title_ru.prefix^2", "title_en.prefix^2"},
				},
			},
		)
	}

	if scope != ScopeTitle {
		clauses = append(clauses,

			map[string]any{
				"multi_match": map[string]any{
					"query":  text,
					"fields": []string{"directors^10", "cast^8", "directors_original^10", "cast_original^8"},
					"type":   "phrase",
				},
			},
			map[string]any{
				"multi_match": map[string]any{
					"query":  text,
					"fields": []string{"directors^6", "cast^4", "directors_original^6", "cast_original^4"},
				},
			},
		)
	}

	if scope == ScopeAll {
		clauses = append(clauses, map[string]any{
			"multi_match": map[string]any{
				"query":  text,
				"fields": []string{"genre_names", "synopsis^0.5"},
			},
		})
	}

	return clauses
}

func buildFilmFilters(f Filters) []any {
	if f.empty() {
		return nil
	}

	filters := make([]any, 0, 6)

	if f.Kind != "" {
		filters = append(filters, map[string]any{"term": map[string]any{"kind": f.Kind}})
	}
	if len(f.Genres) > 0 {
		filters = append(filters, map[string]any{
			"terms": map[string]any{"genres": f.Genres},
		})
	}
	if r := rangeClause("year", float64(f.YearFrom), float64(f.YearTo)); r != nil {
		filters = append(filters, r)
	}

	if f.RuntimeFrom > 0 || f.RuntimeTo > 0 {
		from := f.RuntimeFrom
		if from <= 0 {
			from = 1
		}
		if r := rangeClause("runtime", float64(from), float64(f.RuntimeTo)); r != nil {
			filters = append(filters, r)
		}
	}
	if r := rangeClause("tmdb_rating", f.RatingFrom, 0); r != nil {
		filters = append(filters, r)
	}
	if r := rangeClause("local_rating", f.LocalFrom, 0); r != nil {
		filters = append(filters, r)
	}

	return filters
}

func rangeClause(field string, from, to float64) map[string]any {
	bounds := map[string]any{}
	if from > 0 {
		bounds["gte"] = from
	}
	if to > 0 {
		bounds["lte"] = to
	}
	if len(bounds) == 0 {
		return nil
	}
	return map[string]any{"range": map[string]any{field: bounds}}
}

func filmSort(q Query) []any {
	mode := q.Sort
	if mode == "" {
		if q.Text != "" {
			mode = SortRelevance
		} else {
			mode = SortPopularity
		}
	}

	if mode == SortRelevance && q.Text == "" {
		mode = SortPopularity
	}

	if mode == SortRelevance {
		return []any{
			map[string]any{"_score": map[string]any{"order": "desc"}},
			map[string]any{"id": map[string]any{"order": "desc"}},
		}
	}

	order := q.Order
	if !order.Valid() {
		order = naturalOrder(mode)
	}
	field, ok := sortFields[mode]
	if !ok {
		field = sortFields[SortPopularity]
	}

	return []any{
		map[string]any{field: map[string]any{"order": string(order), "missing": "_last"}},
		map[string]any{"id": map[string]any{"order": string(order)}},
	}
}

func naturalOrder(mode SortMode) SortOrder {
	if mode == SortTitle {
		return OrderAsc
	}
	return OrderDesc
}

func (c *Client) SearchPeople(ctx context.Context, q Query) (Page[PersonDoc], error) {
	limit := q.limit()

	match := map[string]any{
		"bool": map[string]any{
			"should": []any{
				map[string]any{
					"match_phrase": map[string]any{
						"name": map[string]any{"query": q.Text, "boost": 8},
					},
				},

				map[string]any{
					"match_phrase": map[string]any{
						"original_name": map[string]any{"query": q.Text, "boost": 8},
					},
				},
				map[string]any{
					"multi_match": map[string]any{
						"query":         q.Text,
						"fields":        []string{"name^4", "original_name^4"},
						"type":          "best_fields",
						"fuzziness":     "AUTO",
						"prefix_length": 2,
					},
				},
				map[string]any{
					"multi_match": map[string]any{
						"query":  q.Text,
						"fields": []string{"name.prefix^2", "original_name.prefix"},
					},
				},
			},
			"minimum_should_match": 1,
		},
	}

	body := map[string]any{
		"size": limit,

		"query": map[string]any{
			"function_score": map[string]any{
				"query": match,
				"field_value_factor": map[string]any{
					"field":    "film_count",
					"modifier": "log1p",
					"factor":   1.5,
					"missing":  0,
				},
				"boost_mode": "multiply",
			},
		},
		"sort": []any{
			map[string]any{"_score": map[string]any{"order": "desc"}},
			map[string]any{"id": map[string]any{"order": "desc"}},
		},
	}
	if len(q.After) > 0 {
		body["search_after"] = q.After
	}

	return runSearch[PersonDoc](ctx, c, c.PeopleIndex(), body, limit, searchOptions{exactTotal: !q.SkipTotal})
}

type searchResponse[T any] struct {
	Hits struct {
		Total struct {
			Value int `json:"value"`
		} `json:"total"`
		Hits []struct {
			Source T     `json:"_source"`
			Sort   []any `json:"sort"`
		} `json:"hits"`
	} `json:"hits"`
	Aggregations struct {
		Genres struct {
			Buckets []struct {
				Key      string `json:"key"`
				DocCount int    `json:"doc_count"`
			} `json:"buckets"`
		} `json:"genres"`
	} `json:"aggregations"`
}

type searchOptions struct {
	exactTotal bool
}

func runSearch[T any](ctx context.Context, c *Client, index string, body map[string]any, limit int, opts searchOptions) (Page[T], error) {
	body["track_total_hits"] = opts.exactTotal

	payload, err := json.Marshal(body)
	if err != nil {
		return Page[T]{}, fmt.Errorf("search: сборка запроса: %w", err)
	}

	var resp searchResponse[T]
	req := esapi.SearchRequest{
		Index: []string{index},
		Body:  bytes.NewReader(payload),

		RequestCache: &cacheResults,
	}
	if err := c.read(req, ctx, &resp); err != nil {
		return Page[T]{}, err
	}

	page := Page[T]{
		Items: make([]T, 0, len(resp.Hits.Hits)),
		Total: resp.Hits.Total.Value,
	}
	for _, hit := range resp.Hits.Hits {
		page.Items = append(page.Items, hit.Source)
	}

	if len(resp.Hits.Hits) == limit {
		page.NextAfter = resp.Hits.Hits[len(resp.Hits.Hits)-1].Sort
	}
	for _, bucket := range resp.Aggregations.Genres.Buckets {
		page.Facets = append(page.Facets, FacetBucket{Key: bucket.Key, Count: bucket.DocCount})
	}

	return page, nil
}
