package search

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

type analysisDef struct {
	Settings struct {
		Analysis struct {
			CharFilter map[string]struct {
				Mappings []string `json:"mappings"`
			} `json:"char_filter"`
			Analyzer map[string]struct {
				CharFilter []string `json:"char_filter"`
			} `json:"analyzer"`
			Normalizer map[string]struct {
				CharFilter []string `json:"char_filter"`
			} `json:"normalizer"`
		} `json:"analysis"`
	} `json:"settings"`
	Mappings struct {
		Properties map[string]struct {
			Analyzer string `json:"analyzer"`
		} `json:"properties"`
	} `json:"mappings"`
}

func parseMapping(t *testing.T, name, raw string) analysisDef {
	t.Helper()
	var def analysisDef
	if err := json.Unmarshal([]byte(raw), &def); err != nil {
		t.Fatalf("%s mapping is not valid JSON: %v", name, err)
	}
	return def
}

func TestMappingsFoldYo(t *testing.T) {
	t.Parallel()
	for name, raw := range map[string]string{"films": filmsMapping, "people": peopleMapping} {
		analysis := parseMapping(t, name, raw).Settings.Analysis
		if !slices.Contains(analysis.CharFilter["yo_fold"].Mappings, "ё => е") {
			t.Errorf("%s: yo_fold char filter is missing", name)
		}
		for analyzer, def := range analysis.Analyzer {
			if !slices.Contains(def.CharFilter, "yo_fold") {
				t.Errorf("%s: analyzer %s doesn't fold ё", name, analyzer)
			}
		}
		for normalizer, def := range analysis.Normalizer {
			if !slices.Contains(def.CharFilter, "yo_fold") {
				t.Errorf("%s: normalizer %s doesn't fold ё", name, normalizer)
			}
		}
	}
}

func TestNameFieldsUseNameAnalyzer(t *testing.T) {
	t.Parallel()
	films := parseMapping(t, "films", filmsMapping).Mappings.Properties
	for _, field := range []string{"directors", "cast", "directors_original", "cast_original"} {
		if prop, ok := films[field]; !ok || prop.Analyzer != "name_text" {
			t.Errorf("films.%s: want name_text analyzer, got %+v (present %v)", field, prop, ok)
		}
	}
	people := parseMapping(t, "people", peopleMapping).Mappings.Properties
	for _, field := range []string{"name", "original_name"} {
		if people[field].Analyzer != "name_text" {
			t.Errorf("people.%s: want name_text analyzer", field)
		}
	}
}

func TestFilmQuerySearchesOriginalNames(t *testing.T) {
	t.Parallel()
	for _, scope := range []Scope{ScopeAll, ScopeCast} {
		encoded, err := json.Marshal(filmClauses("villeneuve", scope))
		if err != nil {
			t.Fatal(err)
		}
		for _, field := range []string{"directors_original", "cast_original"} {
			if !strings.Contains(string(encoded), field) {
				t.Errorf("scope %s: query doesn't search %s", scope, field)
			}
		}
	}
}
