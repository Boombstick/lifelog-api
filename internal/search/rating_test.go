package search

import (
	"encoding/json"
	"testing"
)

func TestUnratedFilmIndexedWithoutRating(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal(FilmDoc{ID: 1, Kind: "movie"})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"tmdb_rating", "local_rating"} {
		if _, ok := fields[name]; ok {
			t.Errorf("%s is indexed for an unrated film: %s", name, raw)
		}
	}
	if fields["local_votes"] != float64(0) {
		t.Errorf("vote counts stay in the document: %s", raw)
	}

	raw, _ = json.Marshal(FilmDoc{ID: 2, TMDBRating: 7.5, TMDBVotes: 100, LocalRating: 8, LocalVotes: 1})
	var rated FilmDoc
	if err := json.Unmarshal(raw, &rated); err != nil {
		t.Fatal(err)
	}
	if rated.TMDBRating != 7.5 || rated.LocalRating != 8 {
		t.Errorf("a rated film keeps its ratings: %s", raw)
	}
}

func TestRatingPatchNullsRatingWithoutVotes(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal(newRatingPatch(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"doc":{"local_rating":null,"local_votes":0}}` {
		t.Errorf("got %s", raw)
	}

	raw, _ = json.Marshal(newRatingPatch(7.5, 2))
	if string(raw) != `{"doc":{"local_rating":7.5,"local_votes":2}}` {
		t.Errorf("got %s", raw)
	}
}
