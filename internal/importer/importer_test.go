package importer

import (
	"context"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"lifelog-api/internal/film"
	"lifelog-api/internal/tmdb"
)

func day(year int, month time.Month, d int) time.Time {
	return time.Date(year, month, d, 0, 0, 0, 0, time.UTC)
}

type stubSource struct {
	discover func(q tmdb.DiscoverQuery) (ids []int, totalPages int, totalResults int, err error)
	calls    []tmdb.DiscoverQuery
	tvCalls  int
}

func (s *stubSource) DiscoverIDs(_ context.Context, q tmdb.DiscoverQuery) ([]int, int, int, error) {
	s.calls = append(s.calls, q)
	return s.discover(q)
}

func (s *stubSource) DiscoverTV(ctx context.Context, q tmdb.DiscoverQuery) ([]int, int, int, error) {
	s.tvCalls++
	return s.DiscoverIDs(ctx, q)
}

func (s *stubSource) Genres(context.Context, string) ([]tmdb.Genre, error)   { return nil, nil }
func (s *stubSource) TVGenres(context.Context, string) ([]tmdb.Genre, error) { return nil, nil }
func (s *stubSource) Movie(context.Context, int) (*tmdb.Movie, error)        { return nil, nil }
func (s *stubSource) TVDetails(context.Context, int) (*tmdb.TVSeries, error) { return nil, nil }
func (s *stubSource) PosterURL(path string) string {
	if path == "" {
		return ""
	}
	return "https://img.test/w500" + path
}

func newTestImporter(src Source) *Importer {
	return New(src, nil, slog.New(slog.DiscardHandler))
}

func TestYearWindows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		from time.Time
		to   time.Time
		want []window
	}{
		{
			name: "период внутри одного года остаётся одним окном",
			from: day(2020, time.March, 5),
			to:   day(2020, time.August, 20),
			want: []window{{from: day(2020, time.March, 5), to: day(2020, time.August, 20)}},
		},
		{
			name: "период через несколько лет режется по календарным годам",
			from: day(2019, time.June, 10),
			to:   day(2021, time.February, 3),
			want: []window{
				{from: day(2019, time.June, 10), to: day(2019, time.December, 31)},
				{from: day(2020, time.January, 1), to: day(2020, time.December, 31)},
				{from: day(2021, time.January, 1), to: day(2021, time.February, 3)},
			},
		},
		{
			name: "ровно один календарный год",
			from: day(2020, time.January, 1),
			to:   day(2020, time.December, 31),
			want: []window{{from: day(2020, time.January, 1), to: day(2020, time.December, 31)}},
		},
		{
			name: "без нижней границы окно не режется",
			from: time.Time{},
			to:   day(2021, time.February, 3),
			want: []window{{from: time.Time{}, to: day(2021, time.February, 3)}},
		},
		{
			name: "перевёрнутый период возвращается как есть",
			from: day(2021, time.January, 1),
			to:   day(2020, time.January, 1),
			want: []window{{from: day(2021, time.January, 1), to: day(2020, time.January, 1)}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := yearWindows(tc.from, tc.to)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("yearWindows(%v, %v) = %v, ожидалось %v", tc.from, tc.to, got, tc.want)
			}
		})
	}
}

func TestMonthWindows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		in       window
		want     []window
		wantYear bool
	}{
		{
			name: "края окна не выходят за исходный период",
			in:   window{from: day(2020, time.January, 15), to: day(2020, time.March, 10)},
			want: []window{
				{from: day(2020, time.January, 15), to: day(2020, time.January, 31)},

				{from: day(2020, time.February, 1), to: day(2020, time.February, 29)},
				{from: day(2020, time.March, 1), to: day(2020, time.March, 10)},
			},
		},
		{
			name: "невисокосный февраль заканчивается 28-м",
			in:   window{from: day(2021, time.February, 1), to: day(2021, time.February, 28)},
			want: []window{{from: day(2021, time.February, 1), to: day(2021, time.February, 28)}},
		},
		{
			name: "период внутри одного месяца даёт одно окно",
			in:   window{from: day(2020, time.July, 3), to: day(2020, time.July, 9)},
			want: []window{{from: day(2020, time.July, 3), to: day(2020, time.July, 9)}},
		},
		{
			name:     "целый год даёт двенадцать окон",
			in:       window{from: day(2020, time.January, 1), to: day(2020, time.December, 31)},
			wantYear: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := monthWindows(tc.in)
			if tc.wantYear {
				if len(got) != 12 {
					t.Fatalf("получено %d окон, ожидалось 12: %v", len(got), got)
				}
				if !got[0].from.Equal(tc.in.from) || !got[11].to.Equal(tc.in.to) {
					t.Errorf("края года поехали: %v … %v", got[0], got[11])
				}
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("monthWindows(%v) = %v, ожидалось %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestMonthWindowsCoverPeriodContinuously(t *testing.T) {
	t.Parallel()

	in := window{from: day(2019, time.November, 17), to: day(2020, time.March, 2)}
	got := monthWindows(in)

	if len(got) == 0 {
		t.Fatal("окон не получилось вовсе")
	}
	if !got[0].from.Equal(in.from) {
		t.Errorf("первое окно начинается с %v, ожидалось %v", got[0].from, in.from)
	}
	if !got[len(got)-1].to.Equal(in.to) {
		t.Errorf("последнее окно кончается %v, ожидалось %v", got[len(got)-1].to, in.to)
	}
	for idx := 1; idx < len(got); idx++ {
		wantStart := got[idx-1].to.AddDate(0, 0, 1)
		if !got[idx].from.Equal(wantStart) {
			t.Errorf("между окнами %d и %d разрыв: %v → %v", idx-1, idx, got[idx-1].to, got[idx].from)
		}
	}
}

func TestCollectWindowPaging(t *testing.T) {
	t.Parallel()

	pages := map[int][]int{
		1: {10, 11, 12},
		2: {13, 14},
	}
	src := &stubSource{discover: func(q tmdb.DiscoverQuery) ([]int, int, int, error) {
		return pages[q.Page], 2, 5, nil
	}}

	var ids []int
	full, err := newTestImporter(src).collectWindow(
		context.Background(),
		window{from: day(2020, time.January, 1), to: day(2020, time.December, 31)},
		Options{},
		map[int]struct{}{},
		&ids,
		true,
	)
	if err != nil {
		t.Fatalf("collectWindow: %v", err)
	}
	if full {
		t.Error("лимита не было, окно не должно считаться заполненным")
	}
	if want := []int{10, 11, 12, 13, 14}; !reflect.DeepEqual(ids, want) {
		t.Errorf("ids = %v, ожидалось %v", ids, want)
	}
	if len(src.calls) != 2 {
		t.Errorf("сделано %d запросов, ожидалось 2", len(src.calls))
	}
}

func TestCollectWindowDoesNotRefetchFirstPage(t *testing.T) {
	t.Parallel()

	src := &stubSource{discover: func(q tmdb.DiscoverQuery) ([]int, int, int, error) {
		return []int{1, 2}, 1, 2, nil
	}}

	var ids []int
	if _, err := newTestImporter(src).collectWindow(
		context.Background(),
		window{from: day(2020, time.January, 1), to: day(2020, time.December, 31)},
		Options{}, map[int]struct{}{}, &ids, true,
	); err != nil {
		t.Fatalf("collectWindow: %v", err)
	}
	if len(src.calls) != 1 {
		t.Fatalf("сделано %d запросов, ожидался 1", len(src.calls))
	}
}

func TestCollectWindowSplitsOversizedWindow(t *testing.T) {
	t.Parallel()

	w := window{from: day(2020, time.February, 1), to: day(2020, time.March, 31)}

	src := &stubSource{}
	src.discover = func(q tmdb.DiscoverQuery) ([]int, int, int, error) {
		switch {
		case q.From.Equal(w.from) && q.To.Equal(w.to):

			return []int{999}, 600, 20_000, nil
		case q.From.Month() == time.February:
			return []int{21, 22}, 1, 40, nil
		case q.From.Month() == time.March:
			return []int{31}, 1, 20, nil
		}
		t.Errorf("неожиданное окно: %v … %v", q.From, q.To)
		return nil, 0, 0, nil
	}

	var ids []int
	full, err := newTestImporter(src).collectWindow(
		context.Background(), w, Options{}, map[int]struct{}{}, &ids, true,
	)
	if err != nil {
		t.Fatalf("collectWindow: %v", err)
	}
	if full {
		t.Error("лимита не было, окно не должно считаться заполненным")
	}

	if want := []int{21, 22, 31}; !reflect.DeepEqual(ids, want) {
		t.Errorf("ids = %v, ожидалось %v", ids, want)
	}
	if len(src.calls) != 3 {
		t.Fatalf("сделано %d запросов, ожидалось 3 (проба + два месяца): %v", len(src.calls), src.calls)
	}
}

func TestCollectWindowDoesNotSplitTwice(t *testing.T) {
	t.Parallel()

	pages := map[int][]int{1: {1, 2}, 2: {3}}
	src := &stubSource{discover: func(q tmdb.DiscoverQuery) ([]int, int, int, error) {
		return pages[q.Page], 2, 50_000, nil
	}}

	var ids []int
	if _, err := newTestImporter(src).collectWindow(
		context.Background(),
		window{from: day(2020, time.February, 1), to: day(2020, time.February, 29)},
		Options{}, map[int]struct{}{}, &ids, false,
	); err != nil {
		t.Fatalf("collectWindow: %v", err)
	}
	if want := []int{1, 2, 3}; !reflect.DeepEqual(ids, want) {
		t.Errorf("ids = %v, ожидалось %v", ids, want)
	}
	if len(src.calls) != 2 {
		t.Errorf("сделано %d запросов, ожидалось 2 — окно порезалось повторно", len(src.calls))
	}
}

func TestCollectWindowLimitStopsTraversal(t *testing.T) {
	t.Parallel()

	pages := map[int][]int{1: {1, 2, 3}, 2: {4, 5, 6}, 3: {7, 8, 9}}
	src := &stubSource{discover: func(q tmdb.DiscoverQuery) ([]int, int, int, error) {
		return pages[q.Page], 3, 9, nil
	}}

	var ids []int
	full, err := newTestImporter(src).collectWindow(
		context.Background(),
		window{from: day(2020, time.January, 1), to: day(2020, time.December, 31)},
		Options{Limit: 4}, map[int]struct{}{}, &ids, true,
	)
	if err != nil {
		t.Fatalf("collectWindow: %v", err)
	}
	if !full {
		t.Error("лимит набран, окно обязано считаться заполненным")
	}
	if want := []int{1, 2, 3, 4}; !reflect.DeepEqual(ids, want) {
		t.Errorf("ids = %v, ожидалось %v", ids, want)
	}

	if len(src.calls) != 2 {
		t.Errorf("сделано %d запросов, ожидалось 2", len(src.calls))
	}
}

func TestCollectWindowSkipsDuplicates(t *testing.T) {
	t.Parallel()

	pages := map[int][]int{
		1: {1, 2, 3},
		2: {3, 2, 4},
	}
	src := &stubSource{discover: func(q tmdb.DiscoverQuery) ([]int, int, int, error) {
		return pages[q.Page], 2, 6, nil
	}}

	var ids []int
	if _, err := newTestImporter(src).collectWindow(
		context.Background(),
		window{from: day(2020, time.January, 1), to: day(2020, time.December, 31)},
		Options{}, map[int]struct{}{}, &ids, true,
	); err != nil {
		t.Fatalf("collectWindow: %v", err)
	}
	if want := []int{1, 2, 3, 4}; !reflect.DeepEqual(ids, want) {
		t.Errorf("ids = %v, ожидалось %v", ids, want)
	}
}

func TestCollectWindowStopsOnEmptyPage(t *testing.T) {
	t.Parallel()

	pages := map[int][]int{1: {1, 2}}
	src := &stubSource{discover: func(q tmdb.DiscoverQuery) ([]int, int, int, error) {
		return pages[q.Page], 5, 100, nil
	}}

	var ids []int
	full, err := newTestImporter(src).collectWindow(
		context.Background(),
		window{from: day(2020, time.January, 1), to: day(2020, time.December, 31)},
		Options{}, map[int]struct{}{}, &ids, true,
	)
	if err != nil {
		t.Fatalf("collectWindow: %v", err)
	}
	if full {
		t.Error("пустая страница — это конец окна, а не набранный лимит")
	}
	if want := []int{1, 2}; !reflect.DeepEqual(ids, want) {
		t.Errorf("ids = %v, ожидалось %v", ids, want)
	}
	if len(src.calls) != 2 {
		t.Errorf("сделано %d запросов, ожидалось 2", len(src.calls))
	}
}

func TestCollectIDsDeduplicatesAcrossWindows(t *testing.T) {
	t.Parallel()

	src := &stubSource{}
	src.discover = func(q tmdb.DiscoverQuery) ([]int, int, int, error) {
		switch q.From.Year() {
		case 2019:
			return []int{1, 2}, 1, 2, nil
		case 2020:
			return []int{2, 3}, 1, 2, nil
		}
		return nil, 0, 0, nil
	}

	ids, err := newTestImporter(src).collectIDs(context.Background(), Options{
		From: day(2019, time.June, 1),
		To:   day(2020, time.June, 1),
	})
	if err != nil {
		t.Fatalf("collectIDs: %v", err)
	}
	if want := []int{1, 2, 3}; !reflect.DeepEqual(ids, want) {
		t.Errorf("ids = %v, ожидалось %v", ids, want)
	}
}

func TestSlugify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "два слова через пробел", value: "Science Fiction", want: "science-fiction"},
		{name: "уже слаг", value: "drama", want: "drama"},
		{name: "знаки препинания схлопываются в один дефис", value: "Action & Adventure", want: "action-adventure"},
		{name: "края не обрастают дефисами", value: "  Comedy!  ", want: "comedy"},
		{name: "цифры сохраняются", value: "Top 250", want: "top-250"},

		{name: "кириллица даёт пустой слаг", value: "Боевик", want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := slugify(tc.value); got != tc.want {
				t.Errorf("slugify(%q) = %q, ожидалось %q", tc.value, got, tc.want)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		limit int
		want  string
	}{
		{name: "короче предела остаётся как есть", value: "кино", limit: 10, want: "кино"},
		{name: "ровно по пределу", value: "кино", limit: 4, want: "кино"},

		{name: "кириллица режется по символам", value: "привет", limit: 3, want: "при"},
		{name: "нулевой предел даёт пустую строку", value: "привет", limit: 0, want: ""},
		{name: "хвостовой пробел убирается", value: "два слова", limit: 4, want: "два"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := truncate(tc.value, tc.limit); got != tc.want {
				t.Errorf("truncate(%q, %d) = %q, ожидалось %q", tc.value, tc.limit, got, tc.want)
			}
		})
	}
}

func TestPrepareCollection(t *testing.T) {
	t.Parallel()

	imp := newTestImporter(&stubSource{})

	t.Run("франшиза переносится в фильм и в отдельную строку", func(t *testing.T) {
		prepared := imp.prepare(&tmdb.Movie{
			ID:    438631,
			Title: "Dune",
			BelongsToCollection: &tmdb.MovieCollection{
				ID:         726871,
				Name:       "Dune Collection",
				PosterPath: "/dune.jpg",
			},
		}, map[int]string{}, 10)

		if prepared.Film.CollectionID == nil || *prepared.Film.CollectionID != 726871 {
			t.Fatalf("ссылка на франшизу: %v", prepared.Film.CollectionID)
		}
		if prepared.Collection == nil {
			t.Fatal("строка франшизы не собрана")
		}
		if prepared.Collection.Name != "Dune Collection" {
			t.Errorf("название: %q", prepared.Collection.Name)
		}

		if prepared.Collection.PosterURL != "https://img.test/w500/dune.jpg" {
			t.Errorf("постер: %q", prepared.Collection.PosterURL)
		}
	})

	t.Run("одиночный фильм остаётся без франшизы", func(t *testing.T) {
		prepared := imp.prepare(&tmdb.Movie{ID: 1, Title: "Одиночка"}, map[int]string{}, 10)

		if prepared.Film.CollectionID != nil {
			t.Errorf("ссылка появилась из ниоткуда: %v", *prepared.Film.CollectionID)
		}
		if prepared.Collection != nil {
			t.Error("строка франшизы собрана для фильма без неё")
		}
	})

	t.Run("нулевой идентификатор франшизы игнорируется", func(t *testing.T) {
		prepared := imp.prepare(&tmdb.Movie{
			ID:                  2,
			BelongsToCollection: &tmdb.MovieCollection{Name: "Пустая"},
		}, map[int]string{}, 10)

		if prepared.Film.CollectionID != nil || prepared.Collection != nil {
			t.Error("пустая франшиза попала в карточку")
		}
	})
}

func TestCollectWindowStopsAtTheTMDBPageCap(t *testing.T) {
	t.Parallel()

	src := &stubSource{discover: func(q tmdb.DiscoverQuery) ([]int, int, int, error) {
		return []int{q.Page}, tmdbMaxPage + 100, 50_000, nil
	}}

	var ids []int

	full, err := newTestImporter(src).collectWindow(
		context.Background(),
		window{from: day(2020, time.January, 1), to: day(2020, time.January, 31)},
		Options{}, map[int]struct{}{}, &ids, false,
	)
	if err != nil {
		t.Fatalf("collectWindow: %v", err)
	}
	if full {
		t.Error("no limit was set, the window must not count as filled")
	}
	if len(src.calls) != tmdbMaxPage {
		t.Errorf("requests = %d, want %d", len(src.calls), tmdbMaxPage)
	}
	if len(ids) != tmdbMaxPage || ids[len(ids)-1] != tmdbMaxPage {
		t.Errorf("ids ended at %v after %d entries", ids[len(ids)-1], len(ids))
	}
}

type stubGenreStorage struct {
	Storage
	stored map[int]string
}

func (s stubGenreStorage) GenreSlugsByTMDBID(context.Context) (map[int]string, error) {
	return s.stored, nil
}

func TestNewSeriesGenresRenamesAClashingSlug(t *testing.T) {
	t.Parallel()

	storage := stubGenreStorage{stored: map[int]string{
		18:    "drama",
		10759: "action-adventure",
	}}
	importer := New(&stubSource{}, storage, slog.New(slog.DiscardHandler))

	slugs := map[int]string{18: "drama", 10762: "action-adventure", 10765: "sci-fi-fantasy"}
	fresh, err := importer.newSeriesGenres(context.Background(), []film.Genre{
		{ID: "drama", TMDBID: 18},
		{ID: "action-adventure", TMDBID: 10762},
		{ID: "sci-fi-fantasy", TMDBID: 10765},
	}, slugs)
	if err != nil {
		t.Fatalf("newSeriesGenres: %v", err)
	}

	if len(fresh) != 2 {
		t.Fatalf("genres to insert = %d, want 2 (id 18 already exists): %+v", len(fresh), fresh)
	}
	byTMDBID := map[int]string{}
	for _, genre := range fresh {
		byTMDBID[genre.TMDBID] = genre.ID
	}
	if byTMDBID[10762] != "action-adventure-tv" {
		t.Errorf("a taken slug must be suffixed, got %q", byTMDBID[10762])
	}
	if byTMDBID[10765] != "sci-fi-fantasy" {
		t.Errorf("a free slug must be left alone, got %q", byTMDBID[10765])
	}

	if slugs[10762] != "action-adventure-tv" || slugs[10765] != "sci-fi-fantasy" {
		t.Errorf("slug map not updated: %v", slugs)
	}
}
