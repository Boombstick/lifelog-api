package importer

import (
	"context"
	"log/slog"
	"sync/atomic"
	"unicode"

	"lifelog-api/internal/film"
	"lifelog-api/internal/tmdb"

	"golang.org/x/sync/errgroup"
)

type LocalizationSource interface {
	Person(ctx context.Context, id int) (*tmdb.Person, error)
	AlternativeTitles(ctx context.Context, movieID int) ([]tmdb.AlternativeTitle, error)
	TVAlternativeTitles(ctx context.Context, tvID int) ([]tmdb.AlternativeTitle, error)
	PosterURL(path string) string
}

type UntranslatedFilm struct {
	ID     uint
	TMDBID int

	Kind film.Kind
}

type IncompletePerson struct {
	ID     uint
	TMDBID int
}

type LocalizationStorage interface {
	UntranslatedFilms(ctx context.Context, afterID uint, limit int) ([]UntranslatedFilm, error)
	IncompletePeople(ctx context.Context, afterID uint, limit int) ([]IncompletePerson, error)
	SetFilmTitleRu(ctx context.Context, id uint, title string) error

	UpdatePerson(ctx context.Context, id uint, name, photoURL string) error
}

type Localizer struct {
	source  LocalizationSource
	storage LocalizationStorage
	logger  *slog.Logger
}

func NewLocalizer(source LocalizationSource, storage LocalizationStorage, logger *slog.Logger) *Localizer {
	if logger == nil {
		logger = slog.Default()
	}
	return &Localizer{source: source, storage: storage, logger: logger}
}

type LocalizeOptions struct {
	Films   bool
	People  bool
	Workers int
	DryRun  bool
}

type LocalizeStats struct {
	FilmsChecked  int
	FilmsFilled   int
	PeopleChecked int
	PeopleFilled  int
	Errors        int
}

func (o LocalizeOptions) workers() int {
	if o.Workers <= 0 {
		return 4
	}
	return o.Workers
}

const batchSize = 500

func (l *Localizer) Run(ctx context.Context, opts LocalizeOptions) (LocalizeStats, error) {
	stats := LocalizeStats{}

	if opts.Films {
		filmStats, err := l.localizeFilms(ctx, opts)
		stats.FilmsChecked, stats.FilmsFilled = filmStats.checked, filmStats.filled
		stats.Errors += filmStats.errors
		if err != nil {
			return stats, err
		}
	}
	if opts.People {
		personStats, err := l.localizePeople(ctx, opts)
		stats.PeopleChecked, stats.PeopleFilled = personStats.checked, personStats.filled
		stats.Errors += personStats.errors
		if err != nil {
			return stats, err
		}
	}
	return stats, nil
}

type pass struct {
	checked int
	filled  int
	errors  int
}

type counters struct {
	filled atomic.Int64
	errors atomic.Int64
}

func (l *Localizer) localizeFilms(ctx context.Context, opts LocalizeOptions) (pass, error) {
	var tally counters
	checked := 0

	var afterID uint
	for {
		films, err := l.storage.UntranslatedFilms(ctx, afterID, batchSize)
		if err != nil {
			return tally.result(checked), err
		}
		if len(films) == 0 {
			return tally.result(checked), nil
		}
		afterID = films[len(films)-1].ID

		group, groupCtx := errgroup.WithContext(ctx)
		group.SetLimit(opts.workers())
		for _, item := range films {
			group.Go(func() error {
				titles, err := alternativeTitles(groupCtx, l.source, item)
				if err != nil {
					l.logger.Warn("альтернативные названия не получены", "tmdb_id", item.TMDBID, "error", err)
					tally.errors.Add(1)
					return nil
				}
				title := russianTitle(titles)
				if title == "" {
					return nil
				}
				if !opts.DryRun {
					if err := l.storage.SetFilmTitleRu(groupCtx, item.ID, title); err != nil {
						return err
					}
				}
				tally.filled.Add(1)
				return nil
			})
		}
		if err := group.Wait(); err != nil {
			return tally.result(checked), err
		}
		checked += len(films)
		l.logger.Info("названия локализованы", "проверено", checked, "заполнено", tally.filled.Load())

		if err := ctx.Err(); err != nil {
			return tally.result(checked), err
		}
	}
}

func (l *Localizer) localizePeople(ctx context.Context, opts LocalizeOptions) (pass, error) {
	var tally counters
	checked := 0

	var afterID uint
	for {
		people, err := l.storage.IncompletePeople(ctx, afterID, batchSize)
		if err != nil {
			return tally.result(checked), err
		}
		if len(people) == 0 {
			return tally.result(checked), nil
		}
		afterID = people[len(people)-1].ID

		group, groupCtx := errgroup.WithContext(ctx)
		group.SetLimit(opts.workers())
		for _, item := range people {
			group.Go(func() error {
				person, err := l.source.Person(groupCtx, item.TMDBID)
				if err != nil {
					l.logger.Warn("человек не получен", "tmdb_id", item.TMDBID, "error", err)
					tally.errors.Add(1)
					return nil
				}
				name := russianName(person.AlsoKnownAs)
				photo := l.source.PosterURL(person.ProfilePath)
				if name == "" && photo == "" {
					return nil
				}
				if !opts.DryRun {
					if err := l.storage.UpdatePerson(groupCtx, item.ID, name, photo); err != nil {
						return err
					}
				}
				tally.filled.Add(1)
				return nil
			})
		}
		if err := group.Wait(); err != nil {
			return tally.result(checked), err
		}
		checked += len(people)
		l.logger.Info("люди дополнены", "проверено", checked, "дополнено", tally.filled.Load())

		if err := ctx.Err(); err != nil {
			return tally.result(checked), err
		}
	}
}

func alternativeTitles(ctx context.Context, source LocalizationSource, item UntranslatedFilm) ([]tmdb.AlternativeTitle, error) {
	if item.Kind == film.KindSeries {
		return source.TVAlternativeTitles(ctx, item.TMDBID)
	}
	return source.AlternativeTitles(ctx, item.TMDBID)
}

func (c *counters) result(checked int) pass {
	return pass{checked: checked, filled: int(c.filled.Load()), errors: int(c.errors.Load())}
}

func russianTitle(titles []tmdb.AlternativeTitle) string {
	for _, item := range titles {
		if item.CountryCode == "RU" && hasCyrillic(item.Title) {
			return item.Title
		}
	}
	return ""
}

func russianName(variants []string) string {
	for _, variant := range variants {
		if hasCyrillic(variant) {
			return variant
		}
	}
	return ""
}

func hasCyrillic(value string) bool {
	for _, symbol := range value {
		if unicode.Is(unicode.Cyrillic, symbol) {
			return true
		}
	}
	return false
}
