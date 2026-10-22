package tmdb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSearchMulti(t *testing.T) {
	t.Parallel()

	var gotQuery map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search/multi" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		gotQuery = map[string]string{
			"query": q.Get("query"), "language": q.Get("language"),
			"include_adult": q.Get("include_adult"), "page": q.Get("page"),
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"page":2,"total_pages":3,"results":[
			{"id":693134,"media_type":"movie","title":"Дюна: Часть вторая","original_title":"Dune: Part Two","release_date":"2024-02-27","poster_path":"/p.jpg","overview":"Пол","vote_count":6200,"vote_average":8.2},
			{"id":1,"media_type":"person","name":"Тимоти Шаламе"},
			{"id":90228,"media_type":"tv","name":"Дюна: Пророчество","original_name":"Dune: Prophecy","first_air_date":"","poster_path":null,"overview":"","vote_count":10,"vote_average":7.1},
			{"id":5,"media_type":"movie","adult":true,"title":"x"}
		]}`))
	}))
	defer server.Close()

	client, err := NewClient(Options{BaseURL: server.URL, Token: "test", RPS: 100, PosterBaseURL: "https://img"})
	if err != nil {
		t.Fatal(err)
	}
	page, err := client.SearchMulti(context.Background(), "Дюна", 2)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]string{"query": "Дюна", "language": "ru-RU", "include_adult": "false", "page": "2"}
	for key, value := range want {
		if gotQuery[key] != value {
			t.Errorf("param %s = %q, want %q", key, gotQuery[key], value)
		}
	}
	if page.Page != 2 || page.TotalPages != 3 {
		t.Errorf("paging %d/%d", page.Page, page.TotalPages)
	}
	if len(page.Items) != 2 {
		t.Fatalf("items %+v, want movie and tv only", page.Items)
	}

	movie := page.Items[0]
	if movie.ID != 693134 || movie.MediaType != "movie" || movie.Title != "Дюна: Часть вторая" ||
		movie.OriginalTitle != "Dune: Part Two" || movie.Date.Year() != 2024 || movie.VoteCount != 6200 {
		t.Errorf("movie %+v", movie)
	}
	if got := client.PosterURLSize(movie.PosterPath, "w342"); got != "https://img/w342/p.jpg" {
		t.Errorf("poster %q", got)
	}

	series := page.Items[1]
	if series.MediaType != "tv" || series.Title != "Дюна: Пророчество" || series.OriginalTitle != "Dune: Prophecy" ||
		!series.Date.IsZero() || series.PosterPath != "" {
		t.Errorf("series %+v", series)
	}
}
