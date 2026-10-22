package postgres

import (
	"reflect"
	"testing"

	"lifelog-api/internal/film"
)

func TestPlanSeasons(t *testing.T) {
	t.Parallel()

	season := func(filmID uint, tmdbID, number int) film.Season {
		return film.Season{FilmID: filmID, TMDBID: tmdbID, Number: number}
	}

	tests := []struct {
		name     string
		existing []storedSeason
		incoming []film.Season
		wantPark []uint
		wantRows int
	}{
		{
			name:     "first import parks nothing",
			incoming: []film.Season{season(1, 100, 0), season(1, 101, 1)},
			wantRows: 2,
		},
		{
			name:     "unchanged refresh parks nothing",
			existing: []storedSeason{{ID: 10, FilmID: 1, TMDBID: 100, Number: 0}, {ID: 11, FilmID: 1, TMDBID: 101, Number: 1}},
			incoming: []film.Season{season(1, 100, 0), season(1, 101, 1)},
			wantRows: 2,
		},
		{
			name:     "season replaced under the same number parks the old row",
			existing: []storedSeason{{ID: 10, FilmID: 1, TMDBID: 100, Number: 3}},
			incoming: []film.Season{season(1, 200, 3)},
			wantPark: []uint{10},
			wantRows: 1,
		},
		{
			name:     "swapped numbers park both rows",
			existing: []storedSeason{{ID: 10, FilmID: 1, TMDBID: 100, Number: 3}, {ID: 11, FilmID: 1, TMDBID: 101, Number: 4}},
			incoming: []film.Season{season(1, 100, 4), season(1, 101, 3)},
			wantPark: []uint{10, 11},
			wantRows: 2,
		},
		{
			name:     "season gone from TMDB without a clash stays untouched",
			existing: []storedSeason{{ID: 12, FilmID: 1, TMDBID: 102, Number: 5}},
			incoming: []film.Season{season(1, 100, 1)},
			wantRows: 1,
		},
		{
			name:     "already parked row is left alone",
			existing: []storedSeason{{ID: 13, FilmID: 1, TMDBID: 103, Number: -13}},
			incoming: []film.Season{season(1, 200, 3)},
			wantRows: 1,
		},
		{
			name:     "same number in another series is no clash",
			existing: []storedSeason{{ID: 10, FilmID: 1, TMDBID: 100, Number: 1}},
			incoming: []film.Season{season(2, 200, 1)},
			wantRows: 1,
		},
		{
			name:     "incoming duplicates are dropped",
			incoming: []film.Season{season(1, 100, 1), season(1, 100, 1), season(1, 101, 1)},
			wantRows: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			park, rows := planSeasons(tc.existing, tc.incoming)
			if !reflect.DeepEqual(park, tc.wantPark) {
				t.Errorf("park = %v, want %v", park, tc.wantPark)
			}
			if len(rows) != tc.wantRows {
				t.Errorf("got %d rows, want %d: %+v", len(rows), tc.wantRows, rows)
			}
		})
	}
}
