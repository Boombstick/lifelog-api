package service

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"lifelog-api/internal/film"
	"lifelog-api/internal/search"
)

func jsonKeys(t *testing.T, value any) []string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func fullDoc() search.FilmDoc {
	return search.FilmDoc{
		ID: 7, TitleRu: "Дюна", TitleEn: "Dune", Kind: "movie", Year: 2021,
		Runtime: 155, PosterURL: "https://image.tmdb.org/p.jpg",
		GenreTitles: []string{"фантастика"}, Directors: []string{"Дени Вильнёв"},
		TMDBRating: 7.8, LocalRating: 8.5, LocalVotes: 2,
	}
}

func TestCatalogItemIsLean(t *testing.T) {
	t.Parallel()

	item := filmCatalogItem(fullDoc())
	if item.Title != "Дюна" {
		t.Errorf("title %q, ожидалось «Дюна»", item.Title)
	}
	want := []string{"id", "kind", "local_rating", "local_votes", "poster_url", "title", "tmdb_rating", "year"}
	if got := jsonKeys(t, item); !slices.Equal(got, want) {
		t.Errorf("ключи %v, ожидались %v", got, want)
	}
	if got := filmCatalogItem(search.FilmDoc{TitleEn: "Dune"}).Title; got != "Dune" {
		t.Errorf("без перевода title %q, ожидалось «Dune»", got)
	}
}

func TestCatalogItemCopiesEveryField(t *testing.T) {
	t.Parallel()

	want := FilmCatalogItem{
		ID: 7, Title: "Дюна", Year: 2021, Kind: "movie", PosterURL: "https://image.tmdb.org/p.jpg",
		TMDBRating: 7.8, LocalRating: 8.5, LocalVotes: 2,
	}
	if got := filmCatalogItem(fullDoc()); got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestFilmRowCopiesEveryField(t *testing.T) {
	t.Parallel()

	card := film.Film{
		ID: 9, TitleRu: "Тьма", TitleEn: "Dark", Kind: film.KindSeries,
		PosterURL: "https://image.tmdb.org/d.jpg", Runtime: 60,
		ReleaseDate:     time.Date(2017, 12, 1, 0, 0, 0, 0, time.UTC),
		TMDBVoteAverage: 8.4, LocalRating: 9, LocalRatingCount: 3,
		Genres: []film.Genre{{NameRu: "драма", NameEn: "Drama"}, {NameEn: "Mystery"}},
	}
	want := FilmRow{
		FilmCatalogItem: FilmCatalogItem{
			ID: 9, Title: "Тьма", Year: 2017, Kind: "series", PosterURL: "https://image.tmdb.org/d.jpg",
			TMDBRating: 8.4, LocalRating: 9, LocalVotes: 3,
		},
		Runtime: 60,
		Genres:  []string{"драма", "Mystery"},
	}
	got := filmRow(card)
	if got.FilmCatalogItem != want.FilmCatalogItem || got.Runtime != want.Runtime || !slices.Equal(got.Genres, want.Genres) {
		t.Errorf("got %+v, want %+v", got, want)
	}

	items := catalogItems([]film.Film{card, {ID: 10, TitleEn: "Other"}})
	if len(items) != 2 || items[0].ID != 9 || items[1].ID != 10 || items[1].Title != "Other" {
		t.Errorf("catalogItems = %+v", items)
	}
}

func TestSearchItemHasNoOriginalTitle(t *testing.T) {
	t.Parallel()

	item := filmSearchItem(fullDoc())
	if item.TitleOrig != "Dune" {
		t.Errorf("TitleOrig %q нужен для выбора блока поиска", item.TitleOrig)
	}
	want := []string{"directors", "genres", "id", "kind", "local_rating", "local_votes", "poster_url", "runtime", "title", "tmdb_rating", "year"}
	if got := jsonKeys(t, item); !slices.Equal(got, want) {
		t.Errorf("ключи %v, ожидались %v", got, want)
	}
}
