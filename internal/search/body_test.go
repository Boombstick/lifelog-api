package search

import (
	"reflect"
	"slices"
	"testing"
)

func excludedFields(t *testing.T, body map[string]any) []string {
	t.Helper()
	source, ok := body["_source"].(map[string]any)
	if !ok {
		t.Fatalf("no _source filter in %v", body)
	}
	fields, ok := source["excludes"].([]string)
	if !ok {
		t.Fatalf("_source has no excludes: %v", source)
	}
	return fields
}

func TestListBodiesDropSearchOnlyFields(t *testing.T) {
	bodies := map[string]map[string]any{
		"search":    filmSearchBody(Query{Text: "дюна"}),
		"catalog":   filmSearchBody(Query{}),
		"recommend": recommendBody(TasteProfile{Genres: map[string]int{"drama": 2}}, 10),
	}
	for name, body := range bodies {
		fields := excludedFields(t, body)
		for _, want := range []string{
			"synopsis", "directors_original", "cast_original", "genre_names",
			"cast", "tmdb_votes", "genres",
		} {
			if !slices.Contains(fields, want) {
				t.Errorf("%s: %q not excluded (%v)", name, want, fields)
			}
		}

		for _, kept := range []string{"title_ru", "title_en", "poster_url", "genre_titles", "directors", "tmdb_rating"} {
			if slices.Contains(fields, kept) {
				t.Errorf("%s: %q is shown but excluded", name, kept)
			}
		}
	}
}

func TestCatalogBodyIncludesOnlyGridFields(t *testing.T) {
	body := filmSearchBody(Query{
		Lean:    true,
		Sort:    SortRuntime,
		Filters: Filters{Genres: []string{"drama"}, RuntimeFrom: 90, RatingFrom: 7},
	})
	source, ok := body["_source"].(map[string]any)
	if !ok {
		t.Fatalf("no _source filter in %v", body)
	}
	if _, ok := source["excludes"]; ok {
		t.Errorf("lean body still carries excludes: %v", source)
	}
	fields, _ := source["includes"].([]string)
	for _, want := range []string{"id", "title_ru", "title_en", "kind", "year", "poster_url", "tmdb_rating", "local_rating", "local_votes"} {
		if !slices.Contains(fields, want) {
			t.Errorf("%q not included (%v)", want, fields)
		}
	}
	for _, dropped := range []string{"genre_titles", "directors", "runtime", "synopsis"} {
		if slices.Contains(fields, dropped) {
			t.Errorf("%q must not reach the grid (%v)", dropped, fields)
		}
	}

	full := filmSearchBody(Query{
		Sort:    SortRuntime,
		Filters: Filters{Genres: []string{"drama"}, RuntimeFrom: 90, RatingFrom: 7},
	})
	if !reflect.DeepEqual(body["query"], full["query"]) || !reflect.DeepEqual(body["sort"], full["sort"]) {
		t.Error("lean projection changed the query or the sort")
	}
}
