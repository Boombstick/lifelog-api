package importer

import (
	"context"
	"strings"
	"time"

	"lifelog-api/internal/film"
	"lifelog-api/internal/tmdb"
)

var seriesTypes = []int{4, 2, 0}

type PreparedSeries struct {
	Details film.SeriesDetails
	Seasons []film.Season
}

func (i *Importer) discover(ctx context.Context, opts Options, q tmdb.DiscoverQuery) ([]int, int, int, error) {
	if opts.Kind == film.KindSeries {
		q.Types = seriesTypes
		return i.source.DiscoverTV(ctx, q)
	}
	return i.source.DiscoverIDs(ctx, q)
}

func (i *Importer) fetch(ctx context.Context, opts Options, id int, genreSlugs map[int]string) (PreparedFilm, error) {
	if opts.Kind == film.KindSeries {
		series, err := i.source.TVDetails(ctx, id)
		if err != nil {
			return PreparedFilm{}, err
		}
		return i.prepareSeries(series, genreSlugs, opts.TopCast), nil
	}
	movie, err := i.source.Movie(ctx, id)
	if err != nil {
		return PreparedFilm{}, err
	}
	return i.prepare(movie, genreSlugs, opts.TopCast), nil
}

func (i *Importer) prepareSeries(series *tmdb.TVSeries, genreSlugs map[int]string, topCast int) PreparedFilm {
	now := time.Now().UTC()

	titleEn, ok := series.Translations.Title("en")
	if !ok {
		titleEn = series.OriginalName
	}
	titleRu, ok := series.Translations.Title("ru")
	if !ok {
		titleRu = russianTitle(series.AlternativeTitles.Titles)
		if titleRu == "" {
			titleRu = series.Name
		}
	}
	if titleRu == "" {
		titleRu = series.OriginalName
	}
	if titleEn == "" {
		titleEn = series.OriginalName
	}

	synopsis := series.Overview
	if synopsis == "" {
		if ru, ok := series.Translations.Overview("ru"); ok {
			synopsis = ru
		} else if en, ok := series.Translations.Overview("en"); ok {
			synopsis = en
		}
	}

	runtime := 0
	if len(series.EpisodeRunTime) > 0 {
		runtime = series.EpisodeRunTime[0]
	}

	prepared := PreparedFilm{
		Adult: series.Adult,
		Film: film.Film{
			TitleEn:         truncate(titleEn, 255),
			TitleRu:         truncate(titleRu, 255),
			Kind:            film.KindSeries,
			Synopsis:        truncate(synopsis, 2000),
			PosterURL:       truncate(i.source.PosterURL(series.PosterPath), 512),
			IMDBID:          series.ExternalIDs.IMDBID,
			TMDBID:          series.ID,
			TMDBPopularity:  float32(series.Popularity),
			Runtime:         runtime,
			TMDBVoteAverage: float32(series.VoteAverage),
			TMDBVoteCount:   series.VoteCount,
			TMDBPosterPath:  truncate(series.PosterPath, 255),
			ReleaseDate:     series.FirstAirDate.Time,
			SyncedAt:        now,
		},
		Series: &PreparedSeries{
			Details: film.SeriesDetails{
				Status:           truncate(series.Status, 32),
				Type:             truncate(series.Type, 32),
				FirstAirDate:     series.FirstAirDate.Ptr(),
				LastAirDate:      series.LastAirDate.Ptr(),
				NumberOfSeasons:  series.NumberOfSeasons,
				NumberOfEpisodes: series.NumberOfEpisodes,
				InProduction:     series.InProduction,
				Networks:         networkNames(series.Networks),
				SyncedAt:         now,
			},
		},
	}
	if series.NextEpisodeToAir != nil {
		prepared.Series.Details.NextAirDate = series.NextEpisodeToAir.AirDate.Ptr()
	}

	for _, season := range series.Seasons {
		if season.ID == 0 {
			continue
		}
		prepared.Series.Seasons = append(prepared.Series.Seasons, film.Season{
			TMDBID:       season.ID,
			Number:       season.SeasonNumber,
			Name:         truncate(season.Name, 255),
			Overview:     truncate(season.Overview, 2000),
			AirDate:      season.AirDate.Ptr(),
			EpisodeCount: season.EpisodeCount,
			PosterURL:    truncate(i.source.PosterURL(season.PosterPath), 512),
			SyncedAt:     now,
		})
	}

	for _, g := range series.Genres {
		if slug, ok := genreSlugs[g.ID]; ok {
			prepared.GenreSlugs = append(prepared.GenreSlugs, slug)
		}
	}

	credits := newCreditCollector(now)
	for idx, creator := range series.CreatedBy {
		credits.add(creator.ID, creator.Name, creator.OriginalName, film.CreditRoleCreator, idx)
	}

	for idx, actor := range series.AggregateCredits.Cast {
		if idx >= topCast {
			break
		}
		credits.add(actor.ID, actor.Name, actor.OriginalName, film.CreditRoleActor, idx)
	}
	prepared.Credits = credits.credits

	return prepared
}

func networkNames(networks []tmdb.TVNetwork) string {
	names := make([]string, 0, len(networks))
	for _, network := range networks {
		if network.Name != "" {
			names = append(names, network.Name)
		}
	}
	return strings.Join(names, ", ")
}
