package imdbimport

import (
	"testing"

	"lifelog-api/internal/film"
	"lifelog-api/internal/tmdb"
)

func TestClassifyTitleType(t *testing.T) {
	t.Parallel()

	cases := map[string]TitleClass{
		"Movie":          ClassMovie,
		"movie":          ClassMovie,
		"TV Movie":       ClassMovie,
		"tvMovie":        ClassMovie,
		"Short":          ClassMovie,
		"short":          ClassMovie,
		"Video":          ClassMovie,
		"video":          ClassMovie,
		"TV Series":      ClassSeries,
		"tvSeries":       ClassSeries,
		"TV Mini Series": ClassSeries,
		"tvMiniSeries":   ClassSeries,
		" TV Series ":    ClassSeries,
		"TV Episode":     ClassUnsupported,
		"tvEpisode":      ClassUnsupported,
		"Video Game":     ClassUnsupported,
		"Podcast Series": ClassUnsupported,
		"TV Special":     ClassUnsupported,
		"":               ClassUnknown,
	}
	for value, want := range cases {
		if got := ClassifyTitleType(value); got != want {
			t.Errorf("%q: %v, want %v", value, got, want)
		}
	}
}

func TestPickCard(t *testing.T) {
	t.Parallel()

	both := tmdb.FindResult{MovieIDs: []int{10}, TVIDs: []int{20}}
	cases := []struct {
		name   string
		found  tmdb.FindResult
		class  TitleClass
		kind   film.Kind
		id     int
		result PickResult
	}{
		{"movie", tmdb.FindResult{MovieIDs: []int{157336}}, ClassMovie, film.KindMovie, 157336, PickFound},

		{"series labelled movie", tmdb.FindResult{TVIDs: []int{1396}}, ClassMovie, film.KindSeries, 1396, PickFound},
		{"movie labelled series", tmdb.FindResult{MovieIDs: []int{5}}, ClassSeries, film.KindMovie, 5, PickFound},
		{"both, series row", both, ClassSeries, film.KindSeries, 20, PickFound},
		{"both, movie row", both, ClassMovie, film.KindMovie, 10, PickFound},
		{"both, no type", both, ClassUnknown, film.KindMovie, 10, PickFound},
		{"episode", tmdb.FindResult{Other: true}, ClassUnknown, "", 0, PickUnsupported},
		{"nothing", tmdb.FindResult{}, ClassMovie, "", 0, PickNotFound},
	}
	for _, tc := range cases {
		kind, id, result := PickCard(tc.found, tc.class)
		if kind != tc.kind || id != tc.id || result != tc.result {
			t.Errorf("%s: %s %d %v, want %s %d %v", tc.name, kind, id, result, tc.kind, tc.id, tc.result)
		}
	}
}

func TestDecide(t *testing.T) {
	t.Parallel()

	none := Snapshot{}
	cases := []struct {
		name     string
		list     ListKind
		kind     film.Kind
		snapshot Snapshot
		aired    int
		want     Action
	}{
		{"movie, nothing", ListRatings, film.KindMovie, none, 0, ActionRateMovie},
		{"movie, only watchlisted", ListRatings, film.KindMovie, Snapshot{Watchlisted: true}, 0, ActionRateMovie},
		{"movie, watched", ListRatings, film.KindMovie, Snapshot{Watched: true}, 0, ActionSkip},
		{"movie, rated", ListRatings, film.KindMovie, Snapshot{Watched: true, Rated: true}, 0, ActionSkip},

		{"series, nothing", ListRatings, film.KindSeries, none, 5, ActionRateSeasons},
		{"series, watchlisted", ListRatings, film.KindSeries, Snapshot{Watchlisted: true}, 5, ActionRateSeasons},
		{"series, one season watching", ListRatings, film.KindSeries, Snapshot{SeasonEntries: 1}, 5, ActionSkip},
		{"series, dropped", ListRatings, film.KindSeries, Snapshot{Dropped: true}, 5, ActionSkip},
		{"series, nothing aired", ListRatings, film.KindSeries, none, 0, ActionWatchlist},
		{"series, nothing aired, already watchlisted", ListRatings, film.KindSeries, Snapshot{Watchlisted: true}, 0, ActionSkip},

		{"watchlist, nothing", ListWatchlist, film.KindMovie, none, 0, ActionWatchlist},
		{"watchlist, series nothing", ListWatchlist, film.KindSeries, none, 3, ActionWatchlist},
		{"watchlist, already watchlisted", ListWatchlist, film.KindMovie, Snapshot{Watchlisted: true}, 0, ActionSkip},
		{"watchlist, watched", ListWatchlist, film.KindMovie, Snapshot{Watched: true}, 0, ActionSkip},
		{"watchlist, series with a season", ListWatchlist, film.KindSeries, Snapshot{SeasonEntries: 2}, 3, ActionSkip},
		{"watchlist, series dropped", ListWatchlist, film.KindSeries, Snapshot{Dropped: true}, 3, ActionSkip},
	}
	for _, tc := range cases {
		if got := Decide(tc.list, tc.kind, tc.snapshot, tc.aired); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}
