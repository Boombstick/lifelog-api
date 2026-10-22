package tmdb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func TestFindByIMDbID(t *testing.T) {
	t.Parallel()

	var gotPath, gotSource string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotSource = r.URL.Query().Get("external_source")
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/find/tt0903747":
			_, _ = w.Write([]byte(`{"movie_results":[],"person_results":[],"tv_results":[{"id":1396,"name":"Во все тяжкие"}],"tv_episode_results":[],"tv_season_results":[]}`))
		case "/find/tt0959621":
			_, _ = w.Write([]byte(`{"movie_results":[],"tv_results":[],"tv_episode_results":[{"id":62085,"show_id":1396}]}`))
		default:
			_, _ = w.Write([]byte(`{"movie_results":[],"tv_results":[]}`))
		}
	}))
	defer server.Close()

	client, err := NewClient(Options{BaseURL: server.URL, Token: "test", RPS: 100})
	if err != nil {
		t.Fatal(err)
	}

	series, err := client.FindByIMDbID(context.Background(), "tt0903747")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/find/tt0903747" || gotSource != "imdb_id" {
		t.Errorf("request %s source=%s", gotPath, gotSource)
	}
	if !slices.Equal(series.TVIDs, []int{1396}) || len(series.MovieIDs) != 0 || series.Other {
		t.Errorf("series result %+v", series)
	}

	episode, err := client.FindByIMDbID(context.Background(), "tt0959621")
	if err != nil {
		t.Fatal(err)
	}
	if !episode.Other || len(episode.TVIDs) != 0 {
		t.Errorf("episode result %+v", episode)
	}

	unknown, err := client.FindByIMDbID(context.Background(), "tt0000000")
	if err != nil {
		t.Fatal(err)
	}
	if len(unknown.MovieIDs)+len(unknown.TVIDs) != 0 || unknown.Other {
		t.Errorf("unknown result %+v", unknown)
	}
}
