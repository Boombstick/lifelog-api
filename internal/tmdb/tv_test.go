package tmdb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type testServer struct {
	mu       sync.Mutex
	requests []*http.Request
}

func newTestClient(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) (*Client, *testServer) {
	t.Helper()
	recorder := &testServer{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder.mu.Lock()
		recorder.requests = append(recorder.requests, r)
		recorder.mu.Unlock()
		handler(w, r)
	}))
	t.Cleanup(server.Close)

	client, err := NewClient(Options{
		BaseURL:       server.URL,
		Token:         "0123456789abcdef0123456789abcdef",
		RPS:           1000,
		PosterBaseURL: "https://image.test/t/p",
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client, recorder
}

func (s *testServer) last(t *testing.T) *http.Request {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.requests) == 0 {
		t.Fatal("no request reached the server")
	}
	return s.requests[len(s.requests)-1]
}

func day(year int, month time.Month, d int) time.Time {
	return time.Date(year, month, d, 0, 0, 0, 0, time.UTC)
}

func TestTVDetailsParsesFixture(t *testing.T) {
	t.Parallel()

	fixture, err := os.ReadFile("testdata/tv_details.json")
	if err != nil {
		t.Fatal(err)
	}
	client, server := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(fixture)
	})

	series, err := client.TVDetails(context.Background(), 1399)
	if err != nil {
		t.Fatalf("TVDetails: %v", err)
	}

	req := server.last(t)
	if req.URL.Path != "/tv/1399" {
		t.Errorf("path = %q, want /tv/1399", req.URL.Path)
	}
	if got := req.URL.Query().Get("append_to_response"); got != "aggregate_credits,alternative_titles,external_ids,translations" {
		t.Errorf("append_to_response = %q", got)
	}
	if got := req.URL.Query().Get("language"); got != "ru-RU" {
		t.Errorf("language = %q, want ru-RU", got)
	}

	if series.Name != "Игра престолов" || series.OriginalName != "Game of Thrones" {
		t.Errorf("names = %q / %q", series.Name, series.OriginalName)
	}
	if !series.FirstAirDate.Equal(day(2011, time.April, 17)) {
		t.Errorf("FirstAirDate = %v", series.FirstAirDate)
	}
	if !series.LastAirDate.Equal(day(2019, time.May, 19)) {
		t.Errorf("LastAirDate = %v", series.LastAirDate)
	}
	if series.NextEpisodeToAir != nil {
		t.Errorf("NextEpisodeToAir = %+v, want nil for an ended series", series.NextEpisodeToAir)
	}
	if series.Status != "Ended" || series.Type != "Scripted" || series.InProduction {
		t.Errorf("status/type/in_production = %q/%q/%v", series.Status, series.Type, series.InProduction)
	}
	if series.NumberOfSeasons != 8 || series.NumberOfEpisodes != 73 {
		t.Errorf("seasons/episodes = %d/%d", series.NumberOfSeasons, series.NumberOfEpisodes)
	}
	if len(series.EpisodeRunTime) != 2 || series.EpisodeRunTime[0] != 60 {
		t.Errorf("EpisodeRunTime = %v", series.EpisodeRunTime)
	}
	if len(series.Networks) != 2 || series.Networks[1].Name != "Sky Atlantic" {
		t.Errorf("Networks = %+v", series.Networks)
	}
	if len(series.CreatedBy) != 2 || series.CreatedBy[0].OriginalName != "David Benioff" {
		t.Errorf("CreatedBy = %+v", series.CreatedBy)
	}

	if len(series.Seasons) != 3 {
		t.Fatalf("got %d seasons, want 3", len(series.Seasons))
	}
	specials := series.Seasons[0]
	if specials.SeasonNumber != 0 || specials.ID != 3627 || specials.EpisodeCount != 300 {
		t.Errorf("specials = %+v", specials)
	}
	if series.Seasons[2].PosterPath != "" {
		t.Errorf("null poster_path = %q, want empty", series.Seasons[2].PosterPath)
	}

	cast := series.AggregateCredits.Cast
	if len(cast) != 3 {
		t.Fatalf("got %d cast members, want 3", len(cast))
	}
	if cast[1].Order != 681 || cast[1].TotalEpisodeCount != 73 {
		t.Errorf("cast[1] = %+v", cast[1])
	}
	if len(cast[0].Roles) != 1 || cast[0].Roles[0].Character != "Tyrion 'The Halfman' Lannister" {
		t.Errorf("cast[0].Roles = %+v", cast[0].Roles)
	}

	if series.ExternalIDs.IMDBID != "tt0944947" {
		t.Errorf("IMDBID = %q", series.ExternalIDs.IMDBID)
	}
	if titles := series.AlternativeTitles.Titles; len(titles) != 2 || titles[1].CountryCode != "RU" {
		t.Errorf("AlternativeTitles = %+v", titles)
	}

	if title, ok := series.Translations.Title("ru"); !ok || title != "Игра престолов" {
		t.Errorf("Title(ru) = %q, %v", title, ok)
	}
	if title, ok := series.Translations.Title("en"); ok {
		t.Errorf("Title(en) = %q, want missing", title)
	}
}

func TestTVDatesEmptyAndNull(t *testing.T) {
	t.Parallel()

	payload := `{
		"id": 1,
		"first_air_date": "",
		"last_air_date": null,
		"next_episode_to_air": {"air_date": ""},
		"seasons": [{"id": 5, "season_number": 1, "air_date": null}, {"id": 6, "season_number": 2, "air_date": "not-a-date"}]
	}`
	var series TVSeries
	if err := json.Unmarshal([]byte(payload), &series); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	dates := map[string]Date{
		"first_air_date":            series.FirstAirDate,
		"last_air_date":             series.LastAirDate,
		"next_episode_to_air":       series.NextEpisodeToAir.AirDate,
		"season air_date null":      series.Seasons[0].AirDate,
		"season air_date malformed": series.Seasons[1].AirDate,
	}
	for name, date := range dates {
		if !date.IsZero() {
			t.Errorf("%s = %v, want zero", name, date.Time)
		}
		if date.Ptr() != nil {
			t.Errorf("%s.Ptr() = %v, want nil", name, *date.Ptr())
		}
	}

	var empty TVSeries
	if err := json.Unmarshal([]byte(`{"id": 2}`), &empty); err != nil {
		t.Fatal(err)
	}
	if empty.FirstAirDate.Ptr() != nil || empty.NextEpisodeToAir != nil {
		t.Errorf("missing dates = %v / %+v", empty.FirstAirDate, empty.NextEpisodeToAir)
	}
}

func TestDatePtrKeepsValue(t *testing.T) {
	t.Parallel()

	var d Date
	if err := json.Unmarshal([]byte(`"2024-02-29"`), &d); err != nil {
		t.Fatal(err)
	}
	ptr := d.Ptr()
	if ptr == nil || !ptr.Equal(day(2024, time.February, 29)) {
		t.Fatalf("Ptr() = %v", ptr)
	}

	*ptr = ptr.AddDate(1, 0, 0)
	if !d.Equal(day(2024, time.February, 29)) {
		t.Errorf("Date changed through Ptr: %v", d.Time)
	}
}

func TestDiscoverTVQuery(t *testing.T) {
	t.Parallel()

	client, server := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"page": 2, "total_pages": 9, "total_results": 180, "results": [{"id": 242551}, {"id": 7}]}`))
	})

	ids, pages, total, err := client.DiscoverTV(context.Background(), DiscoverQuery{
		From:     day(2024, time.January, 1),
		To:       day(2024, time.February, 29),
		MinVotes: 50,
		Page:     2,
		Types:    []int{4, 2, 0},
	})
	if err != nil {
		t.Fatalf("DiscoverTV: %v", err)
	}
	if len(ids) != 2 || ids[0] != 242551 || pages != 9 || total != 180 {
		t.Errorf("DiscoverTV = %v, %d, %d", ids, pages, total)
	}

	req := server.last(t)
	if req.URL.Path != "/discover/tv" {
		t.Errorf("path = %q", req.URL.Path)
	}
	q := req.URL.Query()
	want := map[string]string{
		"first_air_date.gte": "2024-01-01",
		"first_air_date.lte": "2024-02-29",
		"vote_count.gte":     "50",
		"with_type":          "4|2|0",
		"sort_by":            "first_air_date.asc",
		"page":               "2",
		"language":           "ru-RU",
		"include_adult":      "false",
	}
	for key, value := range want {
		if got := q.Get(key); got != value {
			t.Errorf("%s = %q, want %q", key, got, value)
		}
	}
	if q.Has("primary_release_date.gte") {
		t.Error("movie date filter leaked into the series discover")
	}
}

func TestDiscoverMovieQueryUnchanged(t *testing.T) {
	t.Parallel()

	client, server := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"page": 1, "total_pages": 1, "total_results": 0, "results": []}`))
	})

	if _, _, _, err := client.DiscoverIDs(context.Background(), DiscoverQuery{
		From:     day(2020, time.January, 1),
		To:       day(2020, time.December, 31),
		MinVotes: 200,
		Types:    []int{4},
	}); err != nil {
		t.Fatalf("DiscoverIDs: %v", err)
	}

	req := server.last(t)
	q := req.URL.Query()
	if req.URL.Path != "/discover/movie" ||
		q.Get("primary_release_date.gte") != "2020-01-01" ||
		q.Get("primary_release_date.lte") != "2020-12-31" ||
		q.Get("sort_by") != "primary_release_date.asc" ||
		q.Get("vote_count.gte") != "200" ||
		q.Get("page") != "1" {
		t.Errorf("movie discover request = %s?%s", req.URL.Path, req.URL.RawQuery)
	}
	if q.Has("with_type") {
		t.Error("with_type must not reach /discover/movie")
	}
}

func TestTVAlternativeTitlesReadsResults(t *testing.T) {
	t.Parallel()

	client, server := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id": 5, "results": [{"iso_3166_1": "RU", "title": "Игра тронов", "type": ""}]}`))
	})

	titles, err := client.TVAlternativeTitles(context.Background(), 5)
	if err != nil {
		t.Fatalf("TVAlternativeTitles: %v", err)
	}
	if len(titles) != 1 || titles[0].Title != "Игра тронов" {
		t.Errorf("titles = %+v", titles)
	}
	if req := server.last(t); req.URL.Path != "/tv/5/alternative_titles" || req.URL.Query().Get("country") != "RU" {
		t.Errorf("request = %s?%s", req.URL.Path, req.URL.RawQuery)
	}
}

func TestTVRecommendationsPath(t *testing.T) {
	t.Parallel()

	client, server := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"results": [{"id": 94997, "media_type": "tv"}, {"id": 71912, "media_type": "tv"}]}`))
	})

	ids, err := client.TVRecommendations(context.Background(), 1399)
	if err != nil {
		t.Fatalf("TVRecommendations: %v", err)
	}
	if len(ids) != 2 || ids[0] != 94997 {
		t.Errorf("ids = %v", ids)
	}
	if req := server.last(t); req.URL.Path != "/tv/1399/recommendations" {
		t.Errorf("path = %q", req.URL.Path)
	}
}

func TestRateTVPostsToTVEndpoint(t *testing.T) {
	t.Parallel()

	var (
		mu    sync.Mutex
		posts []string
	)
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/guest_session/new") {
			expires := time.Now().Add(time.Hour).UTC().Format("2006-01-02 15:04:05 MST")
			_, _ = w.Write([]byte(`{"success": true, "guest_session_id": "guest", "expires_at": "` + expires + `"}`))
			return
		}
		mu.Lock()
		posts = append(posts, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.WriteHeader(http.StatusCreated)
	})

	if err := client.RateTV(context.Background(), 1399, 8); err != nil {
		t.Fatalf("RateTV: %v", err)
	}
	if err := client.RateMovie(context.Background(), 1399, 8); err != nil {
		t.Fatalf("RateMovie: %v", err)
	}

	want := []string{"POST /tv/1399/rating", "POST /movie/1399/rating"}
	if strings.Join(posts, ",") != strings.Join(want, ",") {
		t.Errorf("posts = %v, want %v", posts, want)
	}
}
