package service

import (
	"context"
	"math"
	"slices"
	"strings"
	"time"

	"lifelog-api/internal/film"
)

type FilmCardSeries struct {
	Status           string   `json:"status"`
	Type             string   `json:"type"`
	FirstAirDate     string   `json:"first_air_date,omitempty"`
	LastAirDate      string   `json:"last_air_date,omitempty"`
	NextAirDate      string   `json:"next_air_date,omitempty"`
	InProduction     bool     `json:"in_production"`
	NumberOfSeasons  int      `json:"number_of_seasons"`
	NumberOfEpisodes int      `json:"number_of_episodes"`
	Networks         []string `json:"networks"`
}

type FilmCardSeason struct {
	ID           uint   `json:"id"`
	Number       int    `json:"number"`
	Name         string `json:"name"`
	AirDate      string `json:"air_date,omitempty"`
	EpisodeCount int    `json:"episode_count"`
	PosterURL    string `json:"poster_url"`

	Aired bool `json:"aired"`
}

type FilmCardSeasonEntry struct {
	Number int    `json:"number"`
	State  string `json:"state"`
	Rate   *int16 `json:"rate"`
}

type FilmCardProgress struct {
	Watched int `json:"watched"`
	Aired   int `json:"aired"`
}

func utcToday() time.Time {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}

func formatDate(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.Format("2006-01-02")
}

func seriesCard(details *film.SeriesDetails) *FilmCardSeries {
	if details == nil {
		return nil
	}
	card := &FilmCardSeries{
		Status:           details.Status,
		Type:             details.Type,
		FirstAirDate:     formatDate(details.FirstAirDate),
		LastAirDate:      formatDate(details.LastAirDate),
		NextAirDate:      formatDate(details.NextAirDate),
		InProduction:     details.InProduction,
		NumberOfSeasons:  details.NumberOfSeasons,
		NumberOfEpisodes: details.NumberOfEpisodes,
		Networks:         []string{},
	}
	for _, network := range strings.Split(details.Networks, ",") {
		if network = strings.TrimSpace(network); network != "" {
			card.Networks = append(card.Networks, network)
		}
	}
	return card
}

func seasonCards(seasons []film.Season, today time.Time) []FilmCardSeason {
	cards := make([]FilmCardSeason, 0, len(seasons))
	for _, season := range seasons {
		if season.Number < 0 {
			continue
		}
		cards = append(cards, FilmCardSeason{
			ID:           season.ID,
			Number:       season.Number,
			Name:         season.Name,
			AirDate:      formatDate(season.AirDate),
			EpisodeCount: season.EpisodeCount,
			PosterURL:    season.PosterURL,
			Aired:        season.Aired(today),
		})
	}
	return cards
}

func seriesRatingOf(entries []film.SeasonEntryView) *float64 {
	sum, count := 0, 0
	for _, entry := range entries {
		if entry.Number > 0 && entry.Rate != nil {
			sum += int(*entry.Rate)
			count++
		}
	}
	if count == 0 {
		return nil
	}
	rating := math.Round(float64(sum)/float64(count)*10) / 10
	return &rating
}

func roundToHalf(value float64) float64 {
	return math.Max(0.5, math.Min(10, math.Round(value*2)/2))
}

func applySeriesProgress(entry *FilmCardEntry, seasons []film.Season, tracked []film.SeasonEntryView, storedState *string, today time.Time) {
	states := make(map[int]string, len(tracked))
	entry.Seasons = make([]FilmCardSeasonEntry, 0, len(tracked))
	anyWatching := false
	var lowestWatching *int
	for _, season := range tracked {
		states[season.Number] = season.State
		entry.Seasons = append(entry.Seasons, FilmCardSeasonEntry{Number: season.Number, State: season.State, Rate: season.Rate})
		if season.State != film.SeasonStateWatching {
			continue
		}
		anyWatching = true
		if season.Number > 0 && (lowestWatching == nil || season.Number < *lowestWatching) {
			number := season.Number
			lowestWatching = &number
		}
	}

	progress := FilmCardProgress{}
	var firstUnwatched *int
	for _, season := range seasons {
		if season.Number <= 0 || !season.Aired(today) {
			continue
		}
		progress.Aired++
		if states[season.Number] == film.SeasonStateWatched {
			progress.Watched++
		} else if firstUnwatched == nil {
			number := season.Number
			firstUnwatched = &number
		}
	}

	watched := progress.Aired > 0 && progress.Watched == progress.Aired
	entry.SeriesWatched = &watched
	entry.Progress = &progress
	entry.SeriesRating = seriesRatingOf(tracked)
	entry.Rate = nil

	entry.CurrentSeason = lowestWatching
	if entry.CurrentSeason == nil {
		entry.CurrentSeason = firstUnwatched
	}

	entry.SeriesState = nil
	switch {
	case storedState != nil && *storedState == film.SeriesStateDropped:
		dropped := film.SeriesStateDropped
		entry.SeriesState = &dropped
	case anyWatching:
		watching := film.SeriesStateWatching
		entry.SeriesState = &watching
	}
}

func (s *FilmService) requireSeries(ctx context.Context, filmID uint) (int, error) {
	kind, tmdbID, err := s.repo.FilmIdentity(ctx, filmID)
	if err != nil {
		return 0, err
	}
	if kind != film.KindSeries {
		return 0, film.ErrNotSeries
	}
	return tmdbID, nil
}

func (s *FilmService) rejectSeries(ctx context.Context, filmID uint) error {
	kind, _, err := s.repo.FilmIdentity(ctx, filmID)
	if err != nil {
		return err
	}
	if kind == film.KindSeries {
		return film.ErrSeriesDerived
	}
	return nil
}

func (s *FilmService) SetSeason(ctx context.Context, filmID, userID uint, number int, state *string, rate *int16, setRate bool) error {
	tmdbID, err := s.requireSeries(ctx, filmID)
	if err != nil {
		return err
	}
	if state != nil && *state != film.SeasonStateWatching && *state != film.SeasonStateWatched {
		return film.ErrBadSeasonState
	}
	if rate != nil && (*rate < 1 || *rate > 10 || state == nil || *state != film.SeasonStateWatched) {
		return film.ErrSeasonRate
	}
	season, err := s.repo.SeasonByNumber(ctx, filmID, number)
	if err != nil {
		return err
	}
	if state != nil && !season.Aired(utcToday()) {
		return film.ErrSeasonNotAired
	}
	if err := s.repo.SetSeasonEntry(ctx, userID, filmID, *season, state, rate, setRate, time.Now()); err != nil {
		return err
	}
	s.pushLocalRating(filmID)
	if setRate || state == nil {
		s.publishSeriesRating(ctx, userID, filmID, tmdbID)
	}
	return nil
}

func (s *FilmService) SetSeasonRate(ctx context.Context, filmID, userID uint, number int, rate *int16) error {
	if _, err := s.requireSeries(ctx, filmID); err != nil {
		return err
	}
	if rate != nil && (*rate < 1 || *rate > 10) {
		return film.ErrSeasonRate
	}
	tracked, err := s.repo.SeasonEntries(ctx, userID, filmID)
	if err != nil {
		return err
	}
	isWatched := slices.ContainsFunc(tracked, func(e film.SeasonEntryView) bool {
		return e.Number == number && e.State == film.SeasonStateWatched
	})
	if !isWatched {
		if rate == nil {
			return nil
		}
		return film.ErrSeasonRate
	}
	watched := film.SeasonStateWatched
	return s.SetSeason(ctx, filmID, userID, number, &watched, rate, true)
}

func (s *FilmService) MarkAiredSeasons(ctx context.Context, filmID, userID uint, upTo *int) error {
	if _, err := s.requireSeries(ctx, filmID); err != nil {
		return err
	}
	seasons, err := s.repo.Seasons(ctx, filmID)
	if err != nil {
		return err
	}
	aired := film.AiredRegularSeasons(seasons, utcToday(), upTo)
	ids := make([]uint, 0, len(aired))
	for _, season := range aired {
		ids = append(ids, season.ID)
	}
	if err := s.repo.MarkSeasonsWatched(ctx, userID, filmID, ids, time.Now()); err != nil {
		return err
	}
	s.pushLocalRating(filmID)
	return nil
}

func (s *FilmService) SetDropped(ctx context.Context, filmID, userID uint) error {
	if _, err := s.requireSeries(ctx, filmID); err != nil {
		return err
	}
	return s.repo.SetSeriesState(ctx, userID, filmID, film.SeriesStateDropped)
}

func (s *FilmService) ClearDropped(ctx context.Context, filmID, userID uint) error {
	if _, err := s.requireSeries(ctx, filmID); err != nil {
		return err
	}
	return s.repo.ClearSeriesState(ctx, userID, filmID, film.SeriesStateDropped)
}

func (s *FilmService) publishSeriesRating(ctx context.Context, userID, filmID uint, tmdbID int) {
	if s.ratings == nil || tmdbID <= 0 {
		return
	}
	entries, err := s.repo.SeasonEntries(ctx, userID, filmID)
	if err != nil {
		s.logger.Warn("series rating not published", "film_id", filmID, "error", err)
		return
	}
	if rating := seriesRatingOf(entries); rating != nil {
		s.publishRating(film.KindSeries, tmdbID, roundToHalf(*rating))
	}
}

func (s *FilmService) seriesRatings(ctx context.Context, userID uint, entries []film.FilmEntry) (map[uint]float64, error) {
	ids := make([]uint, 0)
	for _, entry := range entries {
		if entry.Film.Kind == film.KindSeries {
			ids = append(ids, entry.FilmID)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	return s.repo.SeriesRatings(ctx, userID, ids)
}
