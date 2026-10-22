package importer

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"

	"lifelog-api/internal/film"
	"lifelog-api/internal/tmdb"
)

func loadSeriesFixture(t *testing.T) *tmdb.TVSeries {
	t.Helper()
	raw, err := os.ReadFile("../tmdb/testdata/tv_details.json")
	if err != nil {
		t.Fatal(err)
	}
	var series tmdb.TVSeries
	if err := json.Unmarshal(raw, &series); err != nil {
		t.Fatal(err)
	}
	return &series
}

func TestPrepareSeriesFromFixture(t *testing.T) {
	t.Parallel()

	series := loadSeriesFixture(t)
	slugs := map[int]string{10765: "sci-fi-fantasy", 18: "drama"}

	got := newTestImporter(&stubSource{}).prepareSeries(series, slugs, 2)

	row := got.Film
	if row.Kind != film.KindSeries || row.TMDBID != 1399 {
		t.Errorf("kind/tmdb = %q/%d", row.Kind, row.TMDBID)
	}
	if row.TitleRu != "Игра престолов" {
		t.Errorf("TitleRu = %q", row.TitleRu)
	}

	if row.TitleEn != "Game of Thrones" {
		t.Errorf("TitleEn = %q", row.TitleEn)
	}
	if row.Synopsis != "Русский синопсис." {
		t.Errorf("Synopsis = %q", row.Synopsis)
	}
	if row.Runtime != 60 {
		t.Errorf("Runtime = %d, want the first episode_run_time", row.Runtime)
	}
	if row.IMDBID != "tt0944947" {
		t.Errorf("IMDBID = %q", row.IMDBID)
	}
	if !row.ReleaseDate.Equal(day(2011, time.April, 17)) {
		t.Errorf("ReleaseDate = %v, want first_air_date", row.ReleaseDate)
	}
	if row.PosterURL != "https://img.test/w500/got.jpg" || row.TMDBPosterPath != "/got.jpg" {
		t.Errorf("poster = %q / %q", row.PosterURL, row.TMDBPosterPath)
	}
	if row.TMDBVoteCount != 27727 || row.TMDBVoteAverage != 8.5 {
		t.Errorf("votes = %d / %v", row.TMDBVoteCount, row.TMDBVoteAverage)
	}
	if row.SyncedAt.IsZero() {
		t.Error("SyncedAt is zero")
	}

	if got.Series == nil {
		t.Fatal("Series is nil")
	}
	details := got.Series.Details
	if details.Status != "Ended" || details.Type != "Scripted" || details.InProduction {
		t.Errorf("status/type/in_production = %q/%q/%v", details.Status, details.Type, details.InProduction)
	}
	if details.NumberOfSeasons != 8 || details.NumberOfEpisodes != 73 {
		t.Errorf("seasons/episodes = %d/%d", details.NumberOfSeasons, details.NumberOfEpisodes)
	}
	if details.FirstAirDate == nil || !details.FirstAirDate.Equal(day(2011, time.April, 17)) {
		t.Errorf("FirstAirDate = %v", details.FirstAirDate)
	}
	if details.LastAirDate == nil || !details.LastAirDate.Equal(day(2019, time.May, 19)) {
		t.Errorf("LastAirDate = %v", details.LastAirDate)
	}
	if details.NextAirDate != nil {
		t.Errorf("NextAirDate = %v, want nil for an ended series", details.NextAirDate)
	}
	if details.Networks != "HBO, Sky Atlantic" {
		t.Errorf("Networks = %q", details.Networks)
	}

	seasons := got.Series.Seasons
	if len(seasons) != 3 {
		t.Fatalf("got %d seasons, want 3", len(seasons))
	}
	if seasons[0].Number != 0 || seasons[0].TMDBID != 3627 || seasons[0].EpisodeCount != 300 {
		t.Errorf("specials = %+v", seasons[0])
	}
	if seasons[1].AirDate == nil || !seasons[1].AirDate.Equal(day(2011, time.April, 17)) || seasons[1].PosterURL != "https://img.test/w500/s1.jpg" {
		t.Errorf("season 1 = %+v", seasons[1])
	}
	if seasons[2].PosterURL != "" {
		t.Errorf("season without poster got %q", seasons[2].PosterURL)
	}

	if want := []string{"sci-fi-fantasy", "drama"}; !reflect.DeepEqual(got.GenreSlugs, want) {
		t.Errorf("GenreSlugs = %v, want %v", got.GenreSlugs, want)
	}

	type credit struct {
		tmdbID int
		role   film.CreditRole
		order  int
	}
	var credits []credit
	for _, c := range got.Credits {
		credits = append(credits, credit{c.Person.TMDBID, c.Role, c.BillingOrder})
	}

	want := []credit{
		{9813, film.CreditRoleCreator, 0},
		{228068, film.CreditRoleCreator, 1},
		{22970, film.CreditRoleActor, 0},
		{1223786, film.CreditRoleActor, 1},
	}
	if !reflect.DeepEqual(credits, want) {
		t.Errorf("credits = %+v, want %+v", credits, want)
	}
	if got.Credits[0].Person.OriginalName != "David Benioff" {
		t.Errorf("creator original name = %q", got.Credits[0].Person.OriginalName)
	}
}

func TestPrepareSeriesRussianTitleFallbacks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		series tmdb.TVSeries
		wantRu string
		wantEn string
	}{
		{
			name: "RU alternative title beats the untranslated name",
			series: tmdb.TVSeries{
				Name:              "오징어 게임",
				OriginalName:      "오징어 게임",
				AlternativeTitles: tmdb.TVAlternativeTitles{Titles: []tmdb.AlternativeTitle{{CountryCode: "RU", Title: "Игра в кальмара"}}},
				Translations:      tmdb.Translations{Translations: []tmdb.Translation{{ISO639_1: "en", Data: tmdb.TranslationData{Name: "Squid Game"}}}},
			},
			wantRu: "Игра в кальмара",
			wantEn: "Squid Game",
		},
		{
			name: "no translation and no alternative title keep the localized name",
			series: tmdb.TVSeries{
				Name:              "Dark",
				OriginalName:      "Dark",
				AlternativeTitles: tmdb.TVAlternativeTitles{Titles: []tmdb.AlternativeTitle{{CountryCode: "RU", Title: "Dark"}}},
			},
			wantRu: "Dark",
			wantEn: "Dark",
		},
		{
			name:   "empty name falls back to the original",
			series: tmdb.TVSeries{OriginalName: "Original"},
			wantRu: "Original",
			wantEn: "Original",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := newTestImporter(&stubSource{}).prepareSeries(&tc.series, nil, 10)
			if got.Film.TitleRu != tc.wantRu || got.Film.TitleEn != tc.wantEn {
				t.Errorf("titles = %q / %q, want %q / %q", got.Film.TitleRu, got.Film.TitleEn, tc.wantRu, tc.wantEn)
			}
		})
	}
}

func TestPrepareSeriesWithoutDates(t *testing.T) {
	t.Parallel()

	var series tmdb.TVSeries
	payload := `{"id": 9, "original_name": "Soon", "first_air_date": "", "last_air_date": null,
		"next_episode_to_air": {"air_date": "2026-10-01"}, "episode_run_time": [],
		"seasons": [{"id": 90, "season_number": 1, "air_date": ""}]}`
	if err := json.Unmarshal([]byte(payload), &series); err != nil {
		t.Fatal(err)
	}

	got := newTestImporter(&stubSource{}).prepareSeries(&series, nil, 10)
	details := got.Series.Details
	if details.FirstAirDate != nil || details.LastAirDate != nil {
		t.Errorf("dates = %v / %v, want nil", details.FirstAirDate, details.LastAirDate)
	}
	if details.NextAirDate == nil || !details.NextAirDate.Equal(day(2026, time.October, 1)) {
		t.Errorf("NextAirDate = %v", details.NextAirDate)
	}
	if got.Series.Seasons[0].AirDate != nil {
		t.Errorf("season AirDate = %v, want nil", got.Series.Seasons[0].AirDate)
	}
	if got.Film.Runtime != 0 {
		t.Errorf("Runtime = %d, want 0", got.Film.Runtime)
	}
}

func TestCollectIDsSeriesUsesDiscoverTV(t *testing.T) {
	t.Parallel()

	src := &stubSource{}
	src.discover = func(q tmdb.DiscoverQuery) ([]int, int, int, error) {
		switch q.From.Year() {
		case 2023:
			return []int{1, 2}, 1, 2, nil
		case 2024:
			return []int{2, 3}, 1, 2, nil
		}
		return nil, 0, 0, nil
	}

	ids, err := newTestImporter(src).collectIDs(context.Background(), Options{
		Kind: film.KindSeries,
		From: day(2023, time.November, 1),
		To:   day(2024, time.February, 29),
	})
	if err != nil {
		t.Fatalf("collectIDs: %v", err)
	}
	if want := []int{1, 2, 3}; !reflect.DeepEqual(ids, want) {
		t.Errorf("ids = %v, want %v", ids, want)
	}
	if src.tvCalls != len(src.calls) || src.tvCalls != 2 {
		t.Errorf("tv calls = %d of %d, want 2 of 2", src.tvCalls, len(src.calls))
	}
	wantWindows := []window{
		{from: day(2023, time.November, 1), to: day(2023, time.December, 31)},
		{from: day(2024, time.January, 1), to: day(2024, time.February, 29)},
	}
	for idx, call := range src.calls {
		if !reflect.DeepEqual(call.Types, seriesTypes) {
			t.Errorf("call %d Types = %v, want %v", idx, call.Types, seriesTypes)
		}
		if !call.From.Equal(wantWindows[idx].from) || !call.To.Equal(wantWindows[idx].to) {
			t.Errorf("call %d window = %v..%v", idx, call.From, call.To)
		}
	}
}

func TestCollectIDsMovieKeepsMovieDiscover(t *testing.T) {
	t.Parallel()

	src := &stubSource{discover: func(tmdb.DiscoverQuery) ([]int, int, int, error) {
		return []int{1}, 1, 1, nil
	}}
	if _, err := newTestImporter(src).collectIDs(context.Background(), Options{
		From: day(2024, time.January, 1),
		To:   day(2024, time.January, 15),
	}); err != nil {
		t.Fatal(err)
	}
	if src.tvCalls != 0 {
		t.Errorf("empty kind reached DiscoverTV %d times", src.tvCalls)
	}
	if src.calls[0].Types != nil {
		t.Errorf("movie discover got Types = %v", src.calls[0].Types)
	}
}
