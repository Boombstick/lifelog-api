package service

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"lifelog-api/internal/film"
)

func day(y int, m time.Month, d int) *time.Time {
	value := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	return &value
}

func rate(v int16) *int16 { return &v }

var today = time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)

func sampleSeasons() []film.Season {
	return []film.Season{
		{ID: 10, Number: 0, Name: "Спецвыпуски", AirDate: day(2020, 1, 1)},
		{ID: 11, Number: 1, AirDate: day(2019, 6, 16)},
		{ID: 12, Number: 2, AirDate: day(2021, 6, 1)},
		{ID: 13, Number: 3, AirDate: day(2024, 4, 7)},
		{ID: 14, Number: 4, AirDate: day(2026, 9, 14)},
		{ID: 15, Number: 5, AirDate: day(2027, 1, 1)},
		{ID: 16, Number: 6},
	}
}

func TestSeasonCardsAiredAndDisplaced(t *testing.T) {
	t.Parallel()
	seasons := append(sampleSeasons(), film.Season{ID: 99, Number: -99, AirDate: day(2019, 1, 1)})
	cards := seasonCards(seasons, today)
	if len(cards) != 7 {
		t.Fatalf("displaced negative seasons must be hidden, got %d cards", len(cards))
	}
	aired := map[int]bool{}
	for _, card := range cards {
		aired[card.Number] = card.Aired
	}
	for number, want := range map[int]bool{0: true, 1: true, 4: true, 5: false, 6: false} {
		if aired[number] != want {
			t.Errorf("season %d aired = %v, want %v", number, aired[number], want)
		}
	}
}

func TestSeriesProgressInProgress(t *testing.T) {
	t.Parallel()
	entry := FilmCardEntry{Rate: rate(5)}
	tracked := []film.SeasonEntryView{
		{Number: 0, State: film.SeasonStateWatched, Rate: rate(2)},
		{Number: 1, State: film.SeasonStateWatched, Rate: rate(8)},
		{Number: 2, State: film.SeasonStateWatched, Rate: rate(9)},
		{Number: 3, State: film.SeasonStateWatching},
	}
	applySeriesProgress(&entry, sampleSeasons(), tracked, nil, today)

	if entry.SeriesWatched == nil || *entry.SeriesWatched {
		t.Errorf("two of four aired seasons watched must not count as watched: %v", entry.SeriesWatched)
	}
	if entry.Progress == nil || *entry.Progress != (FilmCardProgress{Watched: 2, Aired: 4}) {
		t.Errorf("progress %+v", entry.Progress)
	}
	if entry.SeriesRating == nil || *entry.SeriesRating != 8.5 {
		t.Errorf("series rating must average regular seasons only: %v", entry.SeriesRating)
	}
	if entry.Rate != nil {
		t.Error("a series has no rate of its own")
	}
	if entry.SeriesState == nil || *entry.SeriesState != film.SeriesStateWatching {
		t.Errorf("a season being watched makes the series watching: %v", entry.SeriesState)
	}
	if entry.CurrentSeason == nil || *entry.CurrentSeason != 3 {
		t.Errorf("current season must be the one being watched: %v", entry.CurrentSeason)
	}
	if len(entry.Seasons) != 4 || entry.Seasons[3].State != film.SeasonStateWatching {
		t.Errorf("seasons %+v", entry.Seasons)
	}
}

func TestSeriesWatchedWhenAllAiredWatched(t *testing.T) {
	t.Parallel()
	tracked := []film.SeasonEntryView{
		{Number: 1, State: film.SeasonStateWatched},
		{Number: 2, State: film.SeasonStateWatched},
		{Number: 3, State: film.SeasonStateWatched},
		{Number: 4, State: film.SeasonStateWatched},
	}
	entry := FilmCardEntry{}
	applySeriesProgress(&entry, sampleSeasons(), tracked, nil, today)
	if entry.SeriesWatched == nil || !*entry.SeriesWatched || entry.CurrentSeason != nil || entry.SeriesState != nil {
		t.Errorf("all aired watched: watched=%v current=%v state=%v", entry.SeriesWatched, entry.CurrentSeason, entry.SeriesState)
	}

	later := FilmCardEntry{}
	applySeriesProgress(&later, sampleSeasons(), tracked, nil, time.Date(2027, 1, 2, 0, 0, 0, 0, time.UTC))
	if *later.SeriesWatched || later.CurrentSeason == nil || *later.CurrentSeason != 5 {
		t.Errorf("a newly aired season must reopen the series: watched=%v current=%v", *later.SeriesWatched, later.CurrentSeason)
	}
}

func TestDroppedWinsOverWatching(t *testing.T) {
	t.Parallel()
	dropped := film.SeriesStateDropped
	entry := FilmCardEntry{}
	applySeriesProgress(&entry, sampleSeasons(), []film.SeasonEntryView{{Number: 1, State: film.SeasonStateWatching}}, &dropped, today)
	if entry.SeriesState == nil || *entry.SeriesState != film.SeriesStateDropped {
		t.Errorf("stored dropped must be reported: %v", entry.SeriesState)
	}
}

func TestRoundToHalf(t *testing.T) {
	t.Parallel()
	for in, want := range map[float64]float64{8.3: 8.5, 8.2: 8, 9.8: 10, 0.1: 0.5, 7.75: 8} {
		if got := roundToHalf(in); got != want {
			t.Errorf("roundToHalf(%v) = %v, want %v", in, got, want)
		}
	}
}

func TestSeriesCardNetworks(t *testing.T) {
	t.Parallel()
	card := seriesCard(&film.SeriesDetails{Status: "Ended", Networks: "HBO, Max ,", FirstAirDate: day(2019, 6, 16)})
	if !slices.Equal(card.Networks, []string{"HBO", "Max"}) || card.FirstAirDate != "2019-06-16" || card.LastAirDate != "" {
		t.Errorf("unexpected series card %+v", card)
	}
	if seriesCard(nil) != nil {
		t.Error("missing details must give no series block")
	}
}

func TestCreatorsComeFirst(t *testing.T) {
	t.Parallel()
	if !(roleRank("creator") < roleRank("director") && roleRank("director") < roleRank("actor")) {
		t.Error("credit order must be creator, director, actor")
	}
}

type seriesRepo struct {
	*stubFilmRepo
	kind    film.Kind
	seasons []film.Season
	set     []string
	marked  []uint
	tracked []film.SeasonEntryView

	rates []seasonRateCall
}

type seasonRateCall struct {
	rate    *int16
	setRate bool
}

func (r *seriesRepo) SeasonEntries(context.Context, uint, uint) ([]film.SeasonEntryView, error) {
	return r.tracked, nil
}

func (r *seriesRepo) FilmIdentity(context.Context, uint) (film.Kind, int, error) {
	return r.kind, 0, nil
}
func (r *seriesRepo) Seasons(context.Context, uint) ([]film.Season, error) { return r.seasons, nil }
func (r *seriesRepo) SeasonByNumber(_ context.Context, _ uint, number int) (*film.Season, error) {
	for _, season := range r.seasons {
		if season.Number == number {
			return &season, nil
		}
	}
	return nil, film.ErrSeasonNotFound
}
func (r *seriesRepo) SetSeasonEntry(_ context.Context, _, _ uint, season film.Season, state *string, rate *int16, setRate bool, _ time.Time) error {
	value := "null"
	if state != nil {
		value = *state
	}
	r.set = append(r.set, value)
	r.rates = append(r.rates, seasonRateCall{rate, setRate})
	return nil
}

func TestSetSeasonRateKeepsState(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	repo := &seriesRepo{stubFilmRepo: &stubFilmRepo{}, kind: film.KindSeries,
		seasons: []film.Season{
			{ID: 2, Number: 1, AirDate: day(2019, 6, 16)},
			{ID: 3, Number: 2, AirDate: day(2021, 6, 1)},
		},
		tracked: []film.SeasonEntryView{
			{Number: 1, State: film.SeasonStateWatched, Rate: rate(9)},
			{Number: 2, State: film.SeasonStateWatching},
		}}
	svc := NewFilmService(repo, nil, nil, nil)

	if err := svc.SetSeasonRate(ctx, 1, 1, 1, nil); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(repo.set, []string{film.SeasonStateWatched}) || repo.rates[0].rate != nil || !repo.rates[0].setRate {
		t.Fatalf("clearing the rating must keep the season watched, wrote %v %+v", repo.set, repo.rates)
	}
	if err := svc.SetSeasonRate(ctx, 1, 1, 1, rate(7)); err != nil || *repo.rates[1].rate != 7 {
		t.Errorf("rating a watched season: err %v, calls %+v", err, repo.rates)
	}

	repo.set = nil
	if err := svc.SetSeasonRate(ctx, 1, 1, 2, nil); err != nil || len(repo.set) != 0 {
		t.Errorf("clearing a rating a watching season can't have is a no-op: err %v, wrote %v", err, repo.set)
	}
	if err := svc.SetSeasonRate(ctx, 1, 1, 2, rate(8)); !errors.Is(err, film.ErrSeasonRate) {
		t.Errorf("rating a season that is not watched: want ErrSeasonRate, got %v", err)
	}
	if len(repo.set) != 0 {
		t.Errorf("rejected calls must not write, wrote %v", repo.set)
	}
}
func (r *seriesRepo) MarkSeasonsWatched(_ context.Context, _, _ uint, ids []uint, _ time.Time) error {
	r.marked = append(r.marked, ids...)
	return nil
}

func TestSetSeasonRules(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	watched, watching, bogus := film.SeasonStateWatched, film.SeasonStateWatching, "finished"

	movie := &seriesRepo{stubFilmRepo: &stubFilmRepo{}, kind: film.KindMovie}
	if err := NewFilmService(movie, nil, nil, nil).SetSeason(ctx, 1, 1, 1, &watched, nil, false); !errors.Is(err, film.ErrNotSeries) {
		t.Errorf("film: want ErrNotSeries, got %v", err)
	}
	if err := NewFilmService(movie, nil, nil, nil).AddToWatched(ctx, 1, 1); err != nil {
		t.Errorf("film watched must still work, got %v", err)
	}

	upcoming := time.Now().UTC().AddDate(1, 0, 0)
	repo := &seriesRepo{stubFilmRepo: &stubFilmRepo{}, kind: film.KindSeries, seasons: []film.Season{
		{ID: 1, Number: 0, AirDate: day(2019, 1, 1)},
		{ID: 2, Number: 1, AirDate: day(2019, 6, 16)},
		{ID: 3, Number: 2, AirDate: day(2021, 6, 1)},
		{ID: 4, Number: 3, AirDate: &upcoming},
	}}
	svc := NewFilmService(repo, nil, nil, nil)

	cases := []struct {
		name  string
		num   int
		state *string
		rate  *int16
		want  error
	}{
		{"unknown state", 1, &bogus, nil, film.ErrBadSeasonState},
		{"rate on watching", 1, &watching, rate(8), film.ErrSeasonRate},
		{"rate out of range", 1, &watched, rate(11), film.ErrSeasonRate},
		{"rate without state", 1, nil, rate(8), film.ErrSeasonRate},
		{"unaired", 3, &watching, nil, film.ErrSeasonNotAired},
		{"missing season", 9, &watched, nil, film.ErrSeasonNotFound},
	}
	for _, tc := range cases {
		if err := svc.SetSeason(ctx, 1, 1, tc.num, tc.state, tc.rate, tc.rate != nil); !errors.Is(err, tc.want) {
			t.Errorf("%s: want %v, got %v", tc.name, tc.want, err)
		}
	}
	if len(repo.set) != 0 {
		t.Fatalf("rejected calls must not write, wrote %v", repo.set)
	}
	if err := svc.SetSeason(ctx, 1, 1, 3, nil, nil, false); err != nil {
		t.Errorf("clearing an unaired season is harmless, got %v", err)
	}

	if err := svc.AddToWatched(ctx, 1, 1); !errors.Is(err, film.ErrSeriesDerived) {
		t.Errorf("series watched is derived: want ErrSeriesDerived, got %v", err)
	}

	if err := svc.MarkAiredSeasons(ctx, 1, 1, nil); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(repo.marked, []uint{2, 3}) {
		t.Errorf("mark all must skip specials and unaired seasons, marked %v", repo.marked)
	}
}

func TestSeriesRatingRoundsToTheNearestTenth(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		rates []int16
		want  float64
	}{
		"rounds up":     {[]int16{9, 9, 8}, 8.7},
		"rounds down":   {[]int16{8, 8, 9}, 8.3},
		"exact half":    {[]int16{8, 9}, 8.5},
		"single season": {[]int16{7}, 7},
	} {
		t.Run(name, func(t *testing.T) {
			tracked := make([]film.SeasonEntryView, 0, len(c.rates))
			for i, value := range c.rates {
				tracked = append(tracked, film.SeasonEntryView{Number: i + 1, State: film.SeasonStateWatched, Rate: rate(value)})
			}
			got := seriesRatingOf(tracked)
			if got == nil || *got != c.want {
				t.Errorf("rating = %v, want %v", got, c.want)
			}
		})
	}
	if seriesRatingOf(nil) != nil {
		t.Error("with nothing rated the series has no rating")
	}
}

func TestUnairedSeriesIsNotWatched(t *testing.T) {
	t.Parallel()
	announced := []film.Season{
		{ID: 1, Number: 1, AirDate: day(2027, 5, 1)},
		{ID: 2, Number: 2},
	}
	entry := FilmCardEntry{}
	applySeriesProgress(&entry, announced, nil, nil, today)

	if entry.SeriesWatched == nil || *entry.SeriesWatched {
		t.Errorf("an unaired series must not be watched: %v", entry.SeriesWatched)
	}
	if entry.Progress == nil || *entry.Progress != (FilmCardProgress{}) {
		t.Errorf("progress %+v, want zero", entry.Progress)
	}
	if entry.CurrentSeason != nil {
		t.Errorf("there is no current season yet: %v", entry.CurrentSeason)
	}

	specialsOnly := []film.Season{{ID: 3, Number: 0, AirDate: day(2020, 1, 1)}}
	only := FilmCardEntry{}
	applySeriesProgress(&only, specialsOnly, []film.SeasonEntryView{{Number: 0, State: film.SeasonStateWatched}}, nil, today)
	if only.SeriesWatched == nil || *only.SeriesWatched {
		t.Errorf("watching the specials does not finish a series: %v", only.SeriesWatched)
	}
}
