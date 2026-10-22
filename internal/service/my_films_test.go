package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"lifelog-api/internal/film"
)

type stubFilmRepo struct {
	entries    []film.FilmEntry
	entry      *film.FilmEntry
	total      int
	lastFilter film.EntryFilter
	card       *film.Film
	ratings    map[uint]float64

	localRatings map[uint]film.LocalRating
	counts       film.EntryCounts
	countsErr    error
}

func (s *stubFilmRepo) FindEntries(_ context.Context, filter film.EntryFilter) ([]film.FilmEntry, int, error) {
	s.lastFilter = filter
	return s.entries, s.total, nil
}

func (s *stubFilmRepo) EntryCounts(_ context.Context, userID uint) (film.EntryCounts, error) {
	s.lastFilter = film.EntryFilter{UserID: userID}
	return s.counts, s.countsErr
}

func (s *stubFilmRepo) FindAll(context.Context) ([]film.Film, error) { return nil, nil }
func (s *stubFilmRepo) Genres(context.Context) ([]film.Genre, error) { return nil, nil }
func (s *stubFilmRepo) FindById(context.Context, uint, uint) (*film.Film, error) {
	return s.card, nil
}
func (s *stubFilmRepo) FilmWithGenres(context.Context, uint) (*film.Film, error) {
	return s.card, nil
}
func (s *stubFilmRepo) AddRateAndDescription(context.Context, uint, uint, *int16, *string) error {
	return nil
}
func (s *stubFilmRepo) AddToWatchList(context.Context, uint, uint) error          { return nil }
func (s *stubFilmRepo) AddToWatched(context.Context, uint, uint, time.Time) error { return nil }
func (s *stubFilmRepo) RemoveFromWatchList(context.Context, uint, uint) error     { return nil }
func (s *stubFilmRepo) RemoveFromWatched(context.Context, uint, uint) error       { return nil }
func (s *stubFilmRepo) RemoveReview(context.Context, uint, uint) error            { return nil }
func (s *stubFilmRepo) FindEntry(context.Context, uint, uint) (*film.FilmEntry, error) {
	return s.entry, nil
}

func TestMyEntryCopiesEveryField(t *testing.T) {
	t.Parallel()

	watched := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	rate := int16(8)
	description := "хорошо"
	repo := &stubFilmRepo{entry: &film.FilmEntry{
		FilmID: 5, Watchlisted: true, WatchedAt: &watched, Rate: &rate, Description: &description,
	}}
	got, err := NewFilmService(repo, nil, nil, nil).MyEntry(context.Background(), 5, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.FilmID != 5 || !got.Watchlisted || got.WatchedAt != &watched || got.Rate != &rate || got.Description != &description {
		t.Fatalf("got %+v", got)
	}
}

func TestMyFilmsDefaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		request  MyFilmsRequest
		wantSet  film.EntrySet
		wantSort film.EntrySort
	}{
		{
			name:     "без параметров — список к просмотру по добавлению",
			request:  MyFilmsRequest{},
			wantSet:  film.EntrySetWatchlist,
			wantSort: film.EntrySortAdded,
		},
		{
			name:     "у просмотренных свой порядок по умолчанию",
			request:  MyFilmsRequest{Set: "watched"},
			wantSet:  film.EntrySetWatched,
			wantSort: film.EntrySortWatched,
		},
		{
			name:     "у оценённых по умолчанию оценка",
			request:  MyFilmsRequest{Set: "rated"},
			wantSet:  film.EntrySetRated,
			wantSort: film.EntrySortRate,
		},
		{
			name:     "явный порядок побеждает умолчание",
			request:  MyFilmsRequest{Set: "rated", Sort: "title"},
			wantSet:  film.EntrySetRated,
			wantSort: film.EntrySortTitle,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repo := &stubFilmRepo{}
			svc := NewFilmService(repo, nil, nil, nil)

			if _, err := svc.MyFilms(context.Background(), 7, tc.request); err != nil {
				t.Fatalf("MyFilms: %v", err)
			}
			if repo.lastFilter.Set != tc.wantSet {
				t.Errorf("в хранилище ушёл набор %q, ожидался %q", repo.lastFilter.Set, tc.wantSet)
			}
			if repo.lastFilter.Sort != tc.wantSort {
				t.Errorf("в хранилище ушёл порядок %q, ожидался %q", repo.lastFilter.Sort, tc.wantSort)
			}
			if repo.lastFilter.UserID != 7 {
				t.Errorf("UserID = %d, ожидалось 7", repo.lastFilter.UserID)
			}
		})
	}
}

func TestMyFilmsRejectsBadRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		request MyFilmsRequest
		want    error
	}{
		{
			name:    "выдуманный набор",
			request: MyFilmsRequest{Set: "favourites"},
			want:    film.ErrUnknownEntrySet,
		},
		{
			name:    "оценка как порядок списка к просмотру",
			request: MyFilmsRequest{Set: "watchlist", Sort: "rate"},
			want:    film.ErrSortNotApplicable,
		},
		{
			name:    "дата просмотра как порядок списка к просмотру",
			request: MyFilmsRequest{Set: "watchlist", Sort: "watched"},
			want:    film.ErrSortNotApplicable,
		},
		{
			name:    "выдуманный порядок",
			request: MyFilmsRequest{Set: "rated", Sort: "случайно"},
			want:    film.ErrSortNotApplicable,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := NewFilmService(&stubFilmRepo{}, nil, nil, nil).
				MyFilms(context.Background(), 1, tc.request)
			if !errors.Is(err, tc.want) {
				t.Errorf("MyFilms вернул %v, ожидалось %v", err, tc.want)
			}
		})
	}
}

func TestMyFilmsLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		limit     int
		wantLimit int
	}{
		{name: "ноль означает значение по умолчанию", limit: 0, wantLimit: 20},
		{name: "отрицательный приводится к умолчанию", limit: -5, wantLimit: 20},
		{name: "слишком большой обрезается", limit: 51, wantLimit: 20},
		{name: "предельно допустимый проходит", limit: 50, wantLimit: 50},
		{name: "обычный проходит как есть", limit: 5, wantLimit: 5},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repo := &stubFilmRepo{}
			if _, err := NewFilmService(repo, nil, nil, nil).
				MyFilms(context.Background(), 1, MyFilmsRequest{Limit: tc.limit}); err != nil {
				t.Fatalf("MyFilms: %v", err)
			}
			if repo.lastFilter.Limit != tc.wantLimit {
				t.Errorf("в хранилище ушёл limit %d, ожидался %d", repo.lastFilter.Limit, tc.wantLimit)
			}
		})
	}
}

func TestMyFilmsCursor(t *testing.T) {
	t.Parallel()

	watched := time.Date(2026, 3, 4, 10, 30, 0, 0, time.UTC)
	rate := int16(8)

	t.Run("полная страница по дате просмотра отдаёт курсор последней записи", func(t *testing.T) {
		t.Parallel()

		repo := &stubFilmRepo{entries: []film.FilmEntry{
			{FilmID: 10, WatchedAt: &watched},
			{FilmID: 11, WatchedAt: &watched},
		}}
		page, err := NewFilmService(repo, nil, nil, nil).
			MyFilms(context.Background(), 1, MyFilmsRequest{Set: "watched", Limit: 2})
		if err != nil {
			t.Fatalf("MyFilms: %v", err)
		}
		if page.NextCursor == "" {
			t.Fatal("страница заполнена целиком, курсор обязан быть")
		}

		value, id := decodeEntryCursor(page.NextCursor, film.EntrySortWatched)
		if id != 11 {
			t.Errorf("курсор указывает на фильм %d, ожидалось 11", id)
		}
		moment, ok := value.(time.Time)
		if !ok || !moment.Equal(watched) {
			t.Errorf("в курсоре %#v, ожидалось время %v", value, watched)
		}
	})

	t.Run("курсор по оценке переживает кодирование", func(t *testing.T) {
		t.Parallel()

		repo := &stubFilmRepo{entries: []film.FilmEntry{{FilmID: 42, Rate: &rate}}}
		page, err := NewFilmService(repo, nil, nil, nil).
			MyFilms(context.Background(), 1, MyFilmsRequest{Set: "rated", Limit: 1})
		if err != nil {
			t.Fatalf("MyFilms: %v", err)
		}

		value, id := decodeEntryCursor(page.NextCursor, film.EntrySortRate)
		if id != 42 {
			t.Errorf("курсор указывает на фильм %d, ожидалось 42", id)
		}
		if got, ok := value.(float64); !ok || got != 8 {
			t.Errorf("в курсоре %#v, ожидалась оценка 8", value)
		}
	})

	t.Run("неполная страница курсора не даёт", func(t *testing.T) {
		t.Parallel()

		repo := &stubFilmRepo{entries: []film.FilmEntry{{FilmID: 10}}}
		page, err := NewFilmService(repo, nil, nil, nil).
			MyFilms(context.Background(), 1, MyFilmsRequest{Limit: 5})
		if err != nil {
			t.Fatalf("MyFilms: %v", err)
		}

		if page.NextCursor != "" {
			t.Errorf("NextCursor = %q, ожидалась пустая строка", page.NextCursor)
		}
	})

	t.Run("курсор из запроса доезжает до хранилища", func(t *testing.T) {
		t.Parallel()

		repo := &stubFilmRepo{}
		if _, err := NewFilmService(repo, nil, nil, nil).MyFilms(context.Background(), 1, MyFilmsRequest{
			Set:    "rated",
			Cursor: encodeEntryCursor(int16(6), 33),
		}); err != nil {
			t.Fatalf("MyFilms: %v", err)
		}
		if repo.lastFilter.AfterID != 33 {
			t.Errorf("AfterID = %d, ожидалось 33", repo.lastFilter.AfterID)
		}
		if got, ok := repo.lastFilter.AfterValue.(float64); !ok || got != 6 {
			t.Errorf("AfterValue = %#v, ожидалась оценка 6", repo.lastFilter.AfterValue)
		}
	})

	t.Run("испорченный курсор даёт первую страницу", func(t *testing.T) {
		t.Parallel()

		repo := &stubFilmRepo{}
		if _, err := NewFilmService(repo, nil, nil, nil).MyFilms(context.Background(), 1, MyFilmsRequest{
			Cursor: "явно-не-курсор!!",
		}); err != nil {
			t.Fatalf("MyFilms: %v", err)
		}
		if repo.lastFilter.AfterID != 0 || repo.lastFilter.AfterValue != nil {
			t.Errorf("в хранилище ушёл курсор %#v/%d, ожидалась первая страница",
				repo.lastFilter.AfterValue, repo.lastFilter.AfterID)
		}
	})

	t.Run("курсор чужого порядка игнорируется", func(t *testing.T) {
		t.Parallel()

		repo := &stubFilmRepo{}
		if _, err := NewFilmService(repo, nil, nil, nil).MyFilms(context.Background(), 1, MyFilmsRequest{
			Set:    "watched",
			Cursor: encodeEntryCursor(int16(6), 33),
		}); err != nil {
			t.Fatalf("MyFilms: %v", err)
		}
		if repo.lastFilter.AfterID != 0 {
			t.Errorf("AfterID = %d, ожидался 0", repo.lastFilter.AfterID)
		}
	})
}

func TestDecodeEntryCursorGarbage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		cursor string
		sort   film.EntrySort
	}{
		{name: "пустая строка", cursor: "", sort: film.EntrySortAdded},
		{name: "не base64", cursor: "не курсор!!", sort: film.EntrySortAdded},
		{name: "одно значение вместо пары", cursor: encodeCursor([]any{5}), sort: film.EntrySortRate},
		{name: "лишнее значение", cursor: encodeCursor([]any{5, 6, 7}), sort: film.EntrySortRate},
		{name: "идентификатор фильма строкой", cursor: encodeCursor([]any{5, "33"}), sort: film.EntrySortRate},
		{name: "нулевой идентификатор фильма", cursor: encodeCursor([]any{5, 0}), sort: film.EntrySortRate},
		{name: "время вместо оценки", cursor: encodeCursor([]any{"2026-03-04T10:30:00Z", 1}), sort: film.EntrySortRate},
		{name: "оценка вместо времени", cursor: encodeCursor([]any{8, 1}), sort: film.EntrySortAdded},
		{name: "неразбираемое время", cursor: encodeCursor([]any{"вчера", 1}), sort: film.EntrySortWatched},
		{name: "число вместо названия", cursor: encodeCursor([]any{8, 1}), sort: film.EntrySortTitle},
		{name: "json-объект", cursor: base64.RawURLEncoding.EncodeToString([]byte(`{"id":1}`)), sort: film.EntrySortAdded},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			value, id := decodeEntryCursor(tc.cursor, tc.sort)
			if value != nil || id != 0 {
				t.Errorf("decodeEntryCursor(%q, %q) = %#v, %d — ожидалась первая страница",
					tc.cursor, tc.sort, value, id)
			}
		})
	}
}

func TestEncodeEntryCursorEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		value  any
		filmID uint
	}{
		{name: "значения сортировки нет", value: nil, filmID: 7},
		{name: "идентификатора фильма нет", value: int16(8), filmID: 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := encodeEntryCursor(tc.value, tc.filmID); got != "" {
				t.Errorf("encodeEntryCursor(%#v, %d) = %q, ожидалась пустая строка", tc.value, tc.filmID, got)
			}
		})
	}
}

func TestMyFilmItem(t *testing.T) {
	t.Parallel()

	rate := int16(9)
	description := "Хорошо"
	watched := time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)
	added := time.Date(2026, 1, 15, 9, 0, 0, 0, time.UTC)

	entry := film.FilmEntry{
		FilmID:      12,
		Watchlisted: true,
		WatchedAt:   &watched,
		Rate:        &rate,
		Description: &description,
		CreatedAt:   added,
		Film: film.Film{
			TitleRu:          "Дюна",
			TitleEn:          "Dune",
			Kind:             film.KindMovie,
			Runtime:          155,
			PosterURL:        "https://image.tmdb.org/poster.jpg",
			ReleaseDate:      time.Date(2021, 9, 15, 0, 0, 0, 0, time.UTC),
			TMDBVoteAverage:  7.8,
			LocalRating:      9,
			LocalRatingCount: 1,
			Genres: []film.Genre{
				{ID: "science-fiction", NameRu: "фантастика", NameEn: "Science Fiction"},
			},
		},
	}

	item := myFilmItem(entry)

	if item.ID != 12 {
		t.Errorf("ID = %d, ожидалось 12", item.ID)
	}
	if item.Title != "Дюна" || item.TitleOrig != "Dune" {
		t.Errorf("названия %q/%q, ожидались «Дюна»/«Dune»", item.Title, item.TitleOrig)
	}
	if item.Year != 2021 {
		t.Errorf("год %d, ожидался 2021", item.Year)
	}
	if len(item.Genres) != 1 || item.Genres[0] != "фантастика" {
		t.Errorf("жанры %v, ожидалась фантастика", item.Genres)
	}
	wire, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(wire, &fields); err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{"title_original", "genre_slugs", "FilmRow", "FilmCatalogItem"} {
		if _, ok := fields[gone]; ok {
			t.Errorf("в ответе лишнее поле %q", gone)
		}
	}
	if fields["runtime"] != float64(155) || fields["title"] != "Дюна" {
		t.Errorf("поля строки должны лежать на верхнем уровне: %s", wire)
	}
	if item.Rate == nil || *item.Rate != 9 {
		t.Errorf("оценка %v, ожидалась 9", item.Rate)
	}
	if !item.AddedAt.Equal(added) {
		t.Errorf("added_at = %v, ожидалось %v", item.AddedAt, added)
	}
}

func TestMyFilmItemEdges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		card         film.Film
		wantTitle    string
		wantOriginal string
		wantYear     int
	}{
		{
			name:         "русского названия нет — показываем оригинал один раз",
			card:         film.Film{TitleRu: "", TitleEn: "Dune"},
			wantTitle:    "Dune",
			wantOriginal: "",
		},
		{
			name:         "названия совпадают — второе не дублируется",
			card:         film.Film{TitleRu: "Dune", TitleEn: "Dune"},
			wantTitle:    "Dune",
			wantOriginal: "",
		},
		{
			name:      "даты выхода нет — года тоже",
			card:      film.Film{TitleRu: "Без даты"},
			wantTitle: "Без даты",
			wantYear:  0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			item := myFilmItem(film.FilmEntry{Film: tc.card})
			if item.Title != tc.wantTitle {
				t.Errorf("Title = %q, ожидалось %q", item.Title, tc.wantTitle)
			}
			if item.TitleOrig != tc.wantOriginal {
				t.Errorf("TitleOrig = %q, ожидалось %q", item.TitleOrig, tc.wantOriginal)
			}
			if item.Year != tc.wantYear {
				t.Errorf("Year = %d, ожидалось %d", item.Year, tc.wantYear)
			}
		})
	}
}

func TestMyFilmsEmptyPage(t *testing.T) {
	t.Parallel()

	page, err := NewFilmService(&stubFilmRepo{}, nil, nil, nil).
		MyFilms(context.Background(), 1, MyFilmsRequest{})
	if err != nil {
		t.Fatalf("MyFilms: %v", err)
	}

	if page.Items == nil {
		t.Error("Items = nil, ожидался пустой срез")
	}
}

func TestMyFilmsTotalИдётИзХранилища(t *testing.T) {
	t.Parallel()

	repo := &stubFilmRepo{
		entries: []film.FilmEntry{{FilmID: 1}, {FilmID: 2}},
		total:   57,
	}
	page, err := NewFilmService(repo, nil, nil, nil).
		MyFilms(context.Background(), 1, MyFilmsRequest{Limit: 2})
	if err != nil {
		t.Fatalf("MyFilms: %v", err)
	}
	if page.Total != 57 {
		t.Errorf("Total = %d, ожидалось 57", page.Total)
	}
}

func (s *stubFilmRepo) FilmIdentity(context.Context, uint) (film.Kind, int, error) {
	return film.KindMovie, 0, nil
}
func (s *stubFilmRepo) SeriesDetails(context.Context, uint) (*film.SeriesDetails, error) {
	return nil, nil
}
func (s *stubFilmRepo) Seasons(context.Context, uint) ([]film.Season, error) { return nil, nil }
func (s *stubFilmRepo) SeasonByNumber(context.Context, uint, int) (*film.Season, error) {
	return nil, film.ErrSeasonNotFound
}
func (s *stubFilmRepo) SeasonEntries(context.Context, uint, uint) ([]film.SeasonEntryView, error) {
	return nil, nil
}
func (s *stubFilmRepo) SeriesRatings(context.Context, uint, []uint) (map[uint]float64, error) {
	return s.ratings, nil
}
func (s *stubFilmRepo) SetSeasonEntry(context.Context, uint, uint, film.Season, *string, *int16, bool, time.Time) error {
	return nil
}
func (s *stubFilmRepo) MarkSeasonsWatched(context.Context, uint, uint, []uint, time.Time) error {
	return nil
}
func (s *stubFilmRepo) SetSeriesState(context.Context, uint, uint, string) error   { return nil }
func (s *stubFilmRepo) ClearSeriesState(context.Context, uint, uint, string) error { return nil }
func (s *stubFilmRepo) LocalRatings(_ context.Context, ids []uint) ([]film.LocalRating, error) {
	rows := make([]film.LocalRating, 0, len(ids))
	for _, id := range ids {
		if r, ok := s.localRatings[id]; ok {
			rows = append(rows, r)
		}
	}
	return rows, nil
}
