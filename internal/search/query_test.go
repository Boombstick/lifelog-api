package search

import (
	"reflect"
	"strings"
	"testing"
)

func TestBuildFilmFilters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		filters Filters
		want    []any
	}{
		{
			name:    "пустые фильтры не дают условий",
			filters: Filters{},
			want:    nil,
		},
		{
			name:    "жанры складываются в terms",
			filters: Filters{Genres: []string{"drama", "comedy"}},
			want: []any{
				map[string]any{"terms": map[string]any{"genres": []string{"drama", "comedy"}}},
			},
		},
		{
			name:    "год с обеих сторон",
			filters: Filters{YearFrom: 2000, YearTo: 2010},
			want: []any{
				map[string]any{"range": map[string]any{"year": map[string]any{"gte": float64(2000), "lte": float64(2010)}}},
			},
		},
		{
			name:    "только нижняя граница года",
			filters: Filters{YearFrom: 2000},
			want: []any{
				map[string]any{"range": map[string]any{"year": map[string]any{"gte": float64(2000)}}},
			},
		},
		{
			name:    "верхняя граница длительности не пускает карточки без длительности",
			filters: Filters{RuntimeTo: 90},
			want: []any{
				map[string]any{"range": map[string]any{"runtime": map[string]any{"gte": float64(1), "lte": float64(90)}}},
			},
		},
		{
			name:    "только нижняя граница длительности",
			filters: Filters{RuntimeFrom: 120},
			want: []any{
				map[string]any{"range": map[string]any{"runtime": map[string]any{"gte": float64(120)}}},
			},
		},
		{
			name:    "рейтинг TMDB — только снизу",
			filters: Filters{RatingFrom: 7.5},
			want: []any{
				map[string]any{"range": map[string]any{"tmdb_rating": map[string]any{"gte": 7.5}}},
			},
		},
		{
			name:    "локальный рейтинг — отдельное поле, не смешивается с TMDB",
			filters: Filters{LocalFrom: 8},
			want: []any{
				map[string]any{"range": map[string]any{"local_rating": map[string]any{"gte": float64(8)}}},
			},
		},
		{
			name: "порядок условий: жанры, год, длительность, рейтинги",
			filters: Filters{
				Genres:      []string{"drama"},
				YearFrom:    1990,
				RuntimeFrom: 60,
				RatingFrom:  6,
				LocalFrom:   7,
			},
			want: []any{
				map[string]any{"terms": map[string]any{"genres": []string{"drama"}}},
				map[string]any{"range": map[string]any{"year": map[string]any{"gte": float64(1990)}}},
				map[string]any{"range": map[string]any{"runtime": map[string]any{"gte": float64(60)}}},
				map[string]any{"range": map[string]any{"tmdb_rating": map[string]any{"gte": float64(6)}}},
				map[string]any{"range": map[string]any{"local_rating": map[string]any{"gte": float64(7)}}},
			},
		},
		{
			name:    "отрицательные границы условий не дают",
			filters: Filters{YearFrom: -5},
			want:    []any{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := buildFilmFilters(tc.filters)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("buildFilmFilters(%+v) =\n%#v\nожидалось\n%#v", tc.filters, got, tc.want)
			}
		})
	}
}

func TestRangeClause(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		from  float64
		to    float64
		want  map[string]any
		field string
	}{
		{
			name:  "обе границы нулевые",
			field: "year",
			want:  nil,
		},
		{
			name:  "только снизу",
			field: "year",
			from:  2000,
			want:  map[string]any{"range": map[string]any{"year": map[string]any{"gte": float64(2000)}}},
		},
		{
			name:  "только сверху",
			field: "runtime",
			to:    90,
			want:  map[string]any{"range": map[string]any{"runtime": map[string]any{"lte": float64(90)}}},
		},
		{
			name:  "обе границы",
			field: "year",
			from:  2000,
			to:    2010,
			want:  map[string]any{"range": map[string]any{"year": map[string]any{"gte": float64(2000), "lte": float64(2010)}}},
		},
		{
			name:  "отрицательные границы игнорируются",
			field: "year",
			from:  -1,
			to:    -2,
			want:  nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := rangeClause(tc.field, tc.from, tc.to)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("rangeClause(%q, %v, %v) = %#v, ожидалось %#v", tc.field, tc.from, tc.to, got, tc.want)
			}
		})
	}
}

func sortKey(t *testing.T, clause any) string {
	t.Helper()

	m, ok := clause.(map[string]any)
	if !ok {
		t.Fatalf("элемент сортировки не map: %#v", clause)
	}
	if len(m) != 1 {
		t.Fatalf("в элементе сортировки %d ключей, ожидался 1: %#v", len(m), m)
	}
	for key := range m {
		return key
	}
	return ""
}

func TestFilmSort(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		query       Query
		wantPrimary string
	}{
		{
			name:        "по умолчанию с текстом — релевантность",
			query:       Query{Text: "нолан"},
			wantPrimary: "_score",
		},
		{
			name:        "по умолчанию без текста — популярность",
			query:       Query{},
			wantPrimary: "popularity",
		},
		{
			name:        "явная релевантность без текста сводится к популярности",
			query:       Query{Sort: SortRelevance},
			wantPrimary: "popularity",
		},
		{
			name:        "явная релевантность с текстом",
			query:       Query{Sort: SortRelevance, Text: "нолан"},
			wantPrimary: "_score",
		},
		{name: "явная популярность", query: Query{Sort: SortPopularity}, wantPrimary: "popularity"},
		{name: "рейтинг TMDB", query: Query{Sort: SortTMDBRating}, wantPrimary: "tmdb_rating"},
		{
			name:        "прежнее имя рейтинга TMDB",
			query:       Query{Sort: SortRating},
			wantPrimary: "tmdb_rating",
		},
		{name: "локальный рейтинг", query: Query{Sort: SortLocal}, wantPrimary: "local_rating"},
		{name: "год", query: Query{Sort: SortYear}, wantPrimary: "year"},
		{name: "длительность", query: Query{Sort: SortRuntime}, wantPrimary: "runtime"},
		{
			name:        "название",
			query:       Query{Sort: SortTitle},
			wantPrimary: "title_sort",
		},
		{
			name:        "неизвестный режим падает на популярность",
			query:       Query{Sort: SortMode("что-то своё")},
			wantPrimary: "popularity",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := filmSort(tc.query)
			if len(got) != 2 {
				t.Fatalf("в сортировке %d ключей, ожидалось 2: %#v", len(got), got)
			}
			if key := sortKey(t, got[0]); key != tc.wantPrimary {
				t.Errorf("первый ключ сортировки %q, ожидался %q", key, tc.wantPrimary)
			}

			if key := sortKey(t, got[1]); key != "id" {
				t.Errorf("последний ключ сортировки %q, ожидался %q", key, "id")
			}
		})
	}
}

func TestFilmSortPutsMissingValuesLast(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		sort  SortMode
		field string
	}{
		{name: "рейтинг TMDB", sort: SortTMDBRating, field: "tmdb_rating"},
		{name: "локальный рейтинг", sort: SortLocal, field: "local_rating"},
		{name: "год", sort: SortYear, field: "year"},
		{name: "длительность", sort: SortRuntime, field: "runtime"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			primary := filmSort(Query{Sort: tc.sort})[0].(map[string]any)
			opts, ok := primary[tc.field].(map[string]any)
			if !ok {
				t.Fatalf("настройки поля %q не найдены: %#v", tc.field, primary)
			}
			if opts["missing"] != "_last" {
				t.Errorf("missing = %v, ожидалось %q", opts["missing"], "_last")
			}
			if opts["order"] != "desc" {
				t.Errorf("order = %v, ожидалось %q", opts["order"], "desc")
			}
		})
	}
}

func TestFilmSortOrder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		query     Query
		field     string
		wantOrder string
	}{
		{
			name:      "умолчание у рейтинга — по убыванию",
			query:     Query{Sort: SortTMDBRating},
			field:     "tmdb_rating",
			wantOrder: "desc",
		},
		{
			name:      "умолчание у названия — по возрастанию",
			query:     Query{Sort: SortTitle},
			field:     "title_sort",
			wantOrder: "asc",
		},
		{
			name:      "явное возрастание",
			query:     Query{Sort: SortYear, Order: OrderAsc},
			field:     "year",
			wantOrder: "asc",
		},
		{
			name:      "явное убывание у названия",
			query:     Query{Sort: SortTitle, Order: OrderDesc},
			field:     "title_sort",
			wantOrder: "desc",
		},
		{
			name:      "мусор в направлении сводится к естественному",
			query:     Query{Sort: SortRuntime, Order: SortOrder("вверх")},
			field:     "runtime",
			wantOrder: "desc",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := filmSort(tc.query)
			primary := got[0].(map[string]any)
			opts, ok := primary[tc.field].(map[string]any)
			if !ok {
				t.Fatalf("настройки поля %q не найдены: %#v", tc.field, primary)
			}
			if opts["order"] != tc.wantOrder {
				t.Errorf("order = %v, ожидалось %q", opts["order"], tc.wantOrder)
			}

			tie := got[1].(map[string]any)["id"].(map[string]any)
			if tie["order"] != tc.wantOrder {
				t.Errorf("направление id = %v, ожидалось %q", tie["order"], tc.wantOrder)
			}
		})
	}
}

func TestFilmSortIgnoresOrderForRelevance(t *testing.T) {
	t.Parallel()

	primary := filmSort(Query{Text: "нолан", Sort: SortRelevance, Order: OrderAsc})[0].(map[string]any)
	opts := primary["_score"].(map[string]any)
	if opts["order"] != "desc" {
		t.Errorf("order = %v, ожидалось %q", opts["order"], "desc")
	}
}

func TestSortModeValid(t *testing.T) {
	t.Parallel()

	valid := []SortMode{
		SortRelevance, SortPopularity, SortTMDBRating, SortRating,
		SortLocal, SortYear, SortRuntime, SortTitle,
	}
	for _, mode := range valid {
		if !mode.Valid() {
			t.Errorf("режим %q должен быть допустим", mode)
		}
	}
	for _, mode := range []SortMode{"imdb_rating", "", "год"} {
		if mode.Valid() {
			t.Errorf("режим %q не должен быть допустим", mode)
		}
	}
}

func TestExcludeClause(t *testing.T) {
	t.Parallel()

	if got := excludeClause(nil); got != nil {
		t.Errorf("пустой список дал условие %#v", got)
	}
	if got := excludeClause([]uint{}); got != nil {
		t.Errorf("пустой срез дал условие %#v", got)
	}

	clause := excludeClause([]uint{7, 9})
	terms, ok := clause["terms"].(map[string]any)
	if !ok {
		t.Fatalf("условие не terms: %#v", clause)
	}
	ids, ok := terms["id"].([]uint)
	if !ok || len(ids) != 2 || ids[0] != 7 || ids[1] != 9 {
		t.Errorf("идентификаторы %#v, ожидались [7 9]", terms["id"])
	}
}

func TestFilmQueryExcludesWatched(t *testing.T) {
	t.Parallel()

	t.Run("каталог без других условий", func(t *testing.T) {
		t.Parallel()

		query := filmQuery(Query{Filters: Filters{ExcludeIDs: []uint{3}}})
		if _, isMatchAll := query["match_all"]; isMatchAll {
			t.Fatal("исключения потерялись: запрос остался match_all")
		}
		boolQuery := query["bool"].(map[string]any)
		if boolQuery["must_not"] == nil {
			t.Errorf("must_not не выставлен: %#v", boolQuery)
		}
		if boolQuery["filter"] != nil {
			t.Errorf("лишний filter без фильтров: %#v", boolQuery["filter"])
		}
	})

	t.Run("вместе с фильтрами и строкой поиска", func(t *testing.T) {
		t.Parallel()

		query := filmQuery(Query{
			Text:    "нолан",
			Filters: Filters{YearFrom: 2020, ExcludeIDs: []uint{3}},
		})
		boolQuery := query["bool"].(map[string]any)
		if boolQuery["must_not"] == nil {
			t.Errorf("must_not не выставлен: %#v", boolQuery)
		}
		if boolQuery["filter"] == nil {
			t.Errorf("фильтры потерялись: %#v", boolQuery)
		}
		if boolQuery["should"] == nil {
			t.Errorf("строка поиска потерялась: %#v", boolQuery)
		}
	})

	t.Run("без исключений запрос не меняется", func(t *testing.T) {
		t.Parallel()

		query := filmQuery(Query{})
		if _, isMatchAll := query["match_all"]; !isMatchAll {
			t.Errorf("пустой каталог перестал быть match_all: %#v", query)
		}
	})
}

func TestFilmClausesByScope(t *testing.T) {
	t.Parallel()

	fieldsOf := func(clauses []any) []string {
		var all []string
		for _, clause := range clauses {
			match := clause.(map[string]any)["multi_match"].(map[string]any)
			all = append(all, match["fields"].([]string)...)
		}
		return all
	}
	mentions := func(fields []string, needle string) bool {
		for _, field := range fields {
			if strings.Contains(field, needle) {
				return true
			}
		}
		return false
	}

	all := fieldsOf(filmClauses("нолан", ScopeAll))
	if !mentions(all, "title_ru") || !mentions(all, "cast") || !mentions(all, "synopsis") {
		t.Errorf("область all потеряла поля: %v", all)
	}

	titleOnly := fieldsOf(filmClauses("нолан", ScopeTitle))
	if !mentions(titleOnly, "title_ru") {
		t.Errorf("область title не ищет по названию: %v", titleOnly)
	}
	for _, forbidden := range []string{"cast", "directors", "synopsis", "genre_names"} {
		if mentions(titleOnly, forbidden) {
			t.Errorf("область title ищет по %q: %v", forbidden, titleOnly)
		}
	}

	castOnly := fieldsOf(filmClauses("нолан", ScopeCast))
	if !mentions(castOnly, "cast") || !mentions(castOnly, "directors") {
		t.Errorf("область cast не ищет по титрам: %v", castOnly)
	}
	for _, forbidden := range []string{"title_ru", "title_en", "synopsis"} {
		if mentions(castOnly, forbidden) {
			t.Errorf("область cast ищет по %q: %v", forbidden, castOnly)
		}
	}

	if len(fieldsOf(filmClauses("нолан", ""))) != len(all) {
		t.Error("пустая область отличается от all")
	}
}

func TestFilmClausesExactTitleFirst(t *testing.T) {
	t.Parallel()

	exact := filmClauses("Гравитация", ScopeTitle)[0].(map[string]any)["multi_match"].(map[string]any)
	if exact["query"] != "гравитация" {
		t.Errorf("запрос к keyword-полю не приведён к нижнему регистру: %v", exact["query"])
	}
	fields := exact["fields"].([]string)
	if len(fields) != 2 || !strings.HasPrefix(fields[0], "title_ru.raw") {
		t.Errorf("точное совпадение ищется не по raw-подполям: %v", fields)
	}
}

func TestFilmClausesFuzzyKeepsFirstLetters(t *testing.T) {
	t.Parallel()

	for _, clause := range filmClauses("Вильнёв", ScopeAll) {
		match := clause.(map[string]any)["multi_match"].(map[string]any)
		if _, fuzzy := match["fuzziness"]; fuzzy && match["prefix_length"] != 2 {
			t.Errorf("нечёткое совпадение без prefix_length 2: %v", match)
		}
	}
}

func TestFilmFacetsCoverAllGenres(t *testing.T) {
	t.Parallel()

	aggs := filmSearchBody(Query{WithFacets: true})["aggs"].(map[string]any)
	terms := aggs["genres"].(map[string]any)["terms"].(map[string]any)
	if size, _ := terms["size"].(int); size < 30 || terms["field"] != "genres" {
		t.Errorf("genre facet %v", terms)
	}
	if _, ok := filmSearchBody(Query{})["aggs"]; ok {
		t.Error("facets computed without being asked")
	}
}

func TestScopeValid(t *testing.T) {
	t.Parallel()

	for _, scope := range []Scope{"", ScopeAll, ScopeTitle, ScopeCast} {
		if !scope.Valid() {
			t.Errorf("область %q должна быть допустима", scope)
		}
	}
	for _, scope := range []Scope{"director", "название"} {
		if scope.Valid() {
			t.Errorf("область %q не должна быть допустима", scope)
		}
	}
}

func TestQueryLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		limit int
		want  int
	}{
		{name: "ноль означает значение по умолчанию", limit: 0, want: 20},
		{name: "отрицательный тоже", limit: -5, want: 20},

		{name: "выше потолка сбрасывается к умолчанию", limit: 51, want: 20},
		{name: "ровно потолок проходит", limit: 50, want: 50},
		{name: "обычное значение проходит", limit: 10, want: 10},
		{name: "единица проходит", limit: 1, want: 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := (Query{Limit: tc.limit}).limit(); got != tc.want {
				t.Errorf("Query{Limit: %d}.limit() = %d, ожидалось %d", tc.limit, got, tc.want)
			}
		})
	}
}

func TestFilmQueryShape(t *testing.T) {
	t.Parallel()

	t.Run("без текста и фильтров — match_all", func(t *testing.T) {
		t.Parallel()

		got := filmQuery(Query{})
		if _, ok := got["match_all"]; !ok {
			t.Errorf("ожидался match_all, получено %#v", got)
		}
	})

	t.Run("без текста, но с фильтрами — bool только с filter", func(t *testing.T) {
		t.Parallel()

		got := filmQuery(Query{Filters: Filters{Genres: []string{"drama"}}})
		boolPart, ok := got["bool"].(map[string]any)
		if !ok {
			t.Fatalf("ожидался bool, получено %#v", got)
		}
		if _, ok := boolPart["filter"]; !ok {
			t.Errorf("в bool нет filter: %#v", boolPart)
		}

		if _, ok := boolPart["should"]; ok {
			t.Errorf("в запросе без текста не должно быть should: %#v", boolPart)
		}
	})

	t.Run("с текстом фильтры уезжают в filter, а не в should", func(t *testing.T) {
		t.Parallel()

		got := filmQuery(Query{Text: "дюна", Filters: Filters{YearFrom: 2020}})
		boolPart, ok := got["bool"].(map[string]any)
		if !ok {
			t.Fatalf("ожидался bool, получено %#v", got)
		}
		filters, ok := boolPart["filter"].([]any)
		if !ok || len(filters) != 1 {
			t.Fatalf("ожидалось одно условие в filter, получено %#v", boolPart["filter"])
		}

		if boolPart["minimum_should_match"] != 1 {
			t.Errorf("minimum_should_match = %v, ожидалось 1", boolPart["minimum_should_match"])
		}
	})

	t.Run("с текстом без фильтров ключа filter нет вовсе", func(t *testing.T) {
		t.Parallel()

		boolPart := filmQuery(Query{Text: "дюна"})["bool"].(map[string]any)
		if _, ok := boolPart["filter"]; ok {
			t.Errorf("пустой filter не должен попадать в запрос: %#v", boolPart)
		}
	})
}
