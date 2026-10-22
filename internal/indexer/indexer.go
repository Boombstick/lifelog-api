package indexer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"lifelog-api/internal/film"
	"lifelog-api/internal/search"
)

type Catalog interface {
	FilmsBatch(ctx context.Context, afterID uint, limit int) ([]film.Film, error)
	PeopleBatch(ctx context.Context, afterID uint, limit int) ([]PersonRow, error)
}

type Engine interface {
	FilmsIndex() string
	PeopleIndex() string
	EnsureIndices(ctx context.Context) error
	CreateGeneration(ctx context.Context) (search.Generation, error)
	SwitchTo(ctx context.Context, gen search.Generation) ([]string, error)
	DeleteIndices(ctx context.Context, names ...string) error
	IndexFilmsTo(ctx context.Context, index string, docs []search.FilmDoc) ([]search.BulkFailure, error)
	IndexPeopleTo(ctx context.Context, index string, docs []search.PersonDoc) ([]search.BulkFailure, error)
	RefreshIndices(ctx context.Context, indices ...string) error
}

type PersonRow struct {
	ID           uint
	Name         string
	OriginalName string
	PhotoURL     string
	TMDBID       int
	Roles        []string
	FilmCount    int
	Popularity   float32
	KnownFor     []string
}

type Options struct {
	Batch int

	Recreate bool
}

type Stats struct {
	Films  int
	People int

	Failed int
}

var ErrDocumentsFailed = errors.New("indexer: documents refused")

const cleanupTimeout = 30 * time.Second

const maxReasonLen = 300

type Indexer struct {
	catalog Catalog
	engine  Engine
	logger  *slog.Logger
}

func New(catalog Catalog, engine Engine, logger *slog.Logger) *Indexer {
	return &Indexer{catalog: catalog, engine: engine, logger: logger}
}

func (i *Indexer) Run(ctx context.Context, opts Options) (Stats, error) {
	if opts.Recreate {
		return i.recreate(ctx, opts)
	}
	if err := i.engine.EnsureIndices(ctx); err != nil {
		return Stats{}, err
	}
	return i.fill(ctx, opts, search.Generation{Films: i.engine.FilmsIndex(), People: i.engine.PeopleIndex()})
}

func (i *Indexer) recreate(ctx context.Context, opts Options) (Stats, error) {
	gen, err := i.engine.CreateGeneration(ctx)
	if err != nil {
		return Stats{}, err
	}
	i.logger.Info("новое поколение индексов создано", "films_index", gen.Films, "people_index", gen.People)

	stats, err := i.fill(ctx, opts, gen)
	if err == nil {
		var stale []string
		stale, err = i.engine.SwitchTo(ctx, gen)
		if err == nil {
			i.logger.Info("алиасы переключены", "films_index", gen.Films, "people_index", gen.People)
			if deleteErr := i.engine.DeleteIndices(ctx, stale...); deleteErr != nil {
				i.logger.Warn("старые индексы не удалены", "indices", stale, "error", deleteErr)
			}
			return stats, nil
		}
	}

	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()
	if deleteErr := i.engine.DeleteIndices(cleanupCtx, gen.Indices()...); deleteErr != nil {
		i.logger.Error("брошенное поколение не удалено", "indices", gen.Indices(), "error", deleteErr)
	}
	return stats, err
}

func (i *Indexer) fill(ctx context.Context, opts Options, target search.Generation) (Stats, error) {
	batch := opts.Batch
	if batch <= 0 {
		batch = 500
	}

	start := time.Now()
	var stats Stats

	films, failed, err := i.indexFilms(ctx, target.Films, batch)
	stats.Films, stats.Failed = films, failed
	if err != nil {
		return stats, err
	}

	people, failed, err := i.indexPeople(ctx, target.People, batch)
	stats.People, stats.Failed = people, stats.Failed+failed
	if err != nil {
		return stats, err
	}

	if err := i.engine.RefreshIndices(ctx, target.Indices()...); err != nil {
		return stats, err
	}

	if stats.Failed > 0 {
		return stats, fmt.Errorf("%w: %d", ErrDocumentsFailed, stats.Failed)
	}

	i.logger.Info("индексация завершена",
		"films", stats.Films, "people", stats.People, "elapsed", time.Since(start).Round(time.Millisecond))
	return stats, nil
}

func (i *Indexer) indexFilms(ctx context.Context, index string, batch int) (int, int, error) {
	var (
		afterID uint
		total   int
		failed  int
	)

	for {
		films, err := i.catalog.FilmsBatch(ctx, afterID, batch)
		if err != nil {
			return total, failed, err
		}
		if len(films) == 0 {
			return total, failed, nil
		}

		docs := make([]search.FilmDoc, 0, len(films))
		for _, item := range films {
			docs = append(docs, filmToDoc(item))
			afterID = item.ID
		}

		refused, err := i.engine.IndexFilmsTo(ctx, index, docs)
		if err != nil {
			return total, failed, err
		}
		i.logFailures(refused)
		failed += len(refused)
		total += len(docs) - len(refused)
		i.logger.Info("фильмы проиндексированы", "count", len(docs), "всего", total)

		if err := ctx.Err(); err != nil {
			return total, failed, err
		}
	}
}

func (i *Indexer) indexPeople(ctx context.Context, index string, batch int) (int, int, error) {
	var (
		afterID uint
		total   int
		failed  int
	)

	for {
		people, err := i.catalog.PeopleBatch(ctx, afterID, batch)
		if err != nil {
			return total, failed, err
		}
		if len(people) == 0 {
			return total, failed, nil
		}

		docs := make([]search.PersonDoc, 0, len(people))
		for _, row := range people {
			docs = append(docs, personToDoc(row))
			afterID = row.ID
		}

		refused, err := i.engine.IndexPeopleTo(ctx, index, docs)
		if err != nil {
			return total, failed, err
		}
		i.logFailures(refused)
		failed += len(refused)
		total += len(docs) - len(refused)
		i.logger.Info("люди проиндексированы", "count", len(docs), "всего", total)

		if err := ctx.Err(); err != nil {
			return total, failed, err
		}
	}
}

func (i *Indexer) logFailures(refused []search.BulkFailure) {
	for _, item := range refused {
		i.logger.Warn("документ не проиндексирован",
			"id", item.ID, "type", item.Type, "reason", truncate(item.Reason, maxReasonLen))
	}
}

func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

func filmToDoc(item film.Film) search.FilmDoc {
	doc := search.FilmDoc{
		ID:          item.ID,
		TMDBID:      item.TMDBID,
		IMDBID:      item.IMDBID,
		TitleRu:     item.TitleRu,
		TitleEn:     item.TitleEn,
		TitleSort:   sortableTitle(item),
		Kind:        string(item.Kind),
		Synopsis:    item.Synopsis,
		PosterURL:   item.PosterURL,
		Popularity:  item.TMDBPopularity,
		Runtime:     item.Runtime,
		TMDBRating:  item.TMDBVoteAverage,
		TMDBVotes:   item.TMDBVoteCount,
		LocalRating: item.LocalRating,
		LocalVotes:  item.LocalRatingCount,
	}

	if item.TMDBVoteCount == 0 {
		doc.TMDBRating = 0
	}
	if item.LocalRatingCount == 0 {
		doc.LocalRating = 0
	}

	if !item.ReleaseDate.IsZero() && item.ReleaseDate.Year() > 1 {
		release := item.ReleaseDate
		doc.ReleaseDate = &release
		doc.Year = release.Year()
	}

	for _, genre := range item.Genres {
		doc.Genres = append(doc.Genres, genre.ID)
		if genre.NameRu != "" {
			doc.GenreNames = append(doc.GenreNames, genre.NameRu)
			doc.GenreTitles = append(doc.GenreTitles, genre.NameRu)
		}
		if genre.NameEn != "" && genre.NameEn != genre.NameRu {
			doc.GenreNames = append(doc.GenreNames, genre.NameEn)
		}
	}

	for _, credit := range item.Credits {
		name := credit.Person.Name
		if name == "" {
			continue
		}
		original := strings.TrimSpace(credit.Person.OriginalName)
		hasOriginal := original != "" && original != name
		switch credit.Role {
		case film.CreditRoleDirector, film.CreditRoleCreator:
			doc.Directors = append(doc.Directors, name)
			if hasOriginal {
				doc.DirectorsOriginal = append(doc.DirectorsOriginal, original)
			}
		case film.CreditRoleActor:
			doc.Cast = append(doc.Cast, name)
			if hasOriginal {
				doc.CastOriginal = append(doc.CastOriginal, original)
			}
		}
	}

	return doc
}

func sortableTitle(item film.Film) string {
	title := strings.TrimSpace(item.TitleRu)
	if title == "" {
		title = strings.TrimSpace(item.TitleEn)
	}
	return title
}

func personToDoc(row PersonRow) search.PersonDoc {
	return search.PersonDoc{
		ID:           row.ID,
		TMDBID:       row.TMDBID,
		Name:         row.Name,
		OriginalName: row.OriginalName,
		PhotoURL:     row.PhotoURL,
		Roles:        row.Roles,
		FilmCount:    row.FilmCount,
		Popularity:   row.Popularity,
		KnownFor:     row.KnownFor,
		IndexedAt:    time.Now(),
	}
}
