package importer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"lifelog-api/internal/film"
	"lifelog-api/internal/tmdb"

	"golang.org/x/sync/errgroup"
)

type Source interface {
	Genres(ctx context.Context, language string) ([]tmdb.Genre, error)
	TVGenres(ctx context.Context, language string) ([]tmdb.Genre, error)
	DiscoverIDs(ctx context.Context, q tmdb.DiscoverQuery) (ids []int, totalPages int, totalResults int, err error)
	DiscoverTV(ctx context.Context, q tmdb.DiscoverQuery) (ids []int, totalPages int, totalResults int, err error)
	Movie(ctx context.Context, id int) (*tmdb.Movie, error)
	TVDetails(ctx context.Context, id int) (*tmdb.TVSeries, error)
	PosterURL(posterPath string) string
}

type Storage interface {
	UpsertGenres(ctx context.Context, genres []film.Genre) error
	GenreSlugsByTMDBID(ctx context.Context) (map[int]string, error)
	ExistingFilmTMDBIDs(ctx context.Context, kind film.Kind) (map[int]struct{}, error)
	SaveFilms(ctx context.Context, films []PreparedFilm) error
}

type PreparedFilm struct {
	Film       film.Film
	GenreSlugs []string
	Credits    []PreparedCredit

	Collection *film.Collection

	Series *PreparedSeries

	Adult bool
}

type PreparedCredit struct {
	Person       film.Person
	Role         film.CreditRole
	BillingOrder int
}

type Options struct {
	Kind film.Kind

	From time.Time
	To   time.Time

	MinVotes int

	Limit   int
	Workers int

	Batch int

	TopCast int
	DryRun  bool

	Refresh bool

	IDs []int
}

type Stats struct {
	Requested int64
	Fetched   int64
	Saved     int64
	Skipped   int64
	Failed    int64
}

type Importer struct {
	source  Source
	storage Storage
	logger  *slog.Logger
	genres  genreCache
}

func New(source Source, storage Storage, logger *slog.Logger) *Importer {
	return &Importer{source: source, storage: storage, logger: logger}
}

func (i *Importer) Run(ctx context.Context, opts Options) (Stats, error) {
	var stats Stats

	if opts.Workers <= 0 {
		opts.Workers = 4
	}
	if opts.Batch <= 0 {
		opts.Batch = 100
	}
	if opts.TopCast <= 0 {
		opts.TopCast = 10
	}
	switch opts.Kind {
	case "":
		opts.Kind = film.KindMovie
	case film.KindMovie, film.KindSeries:
	default:
		return stats, fmt.Errorf("неизвестный вид карточек %q", opts.Kind)
	}

	genreSlugs, err := i.syncGenres(ctx, opts.Kind, opts.DryRun)
	if err != nil {
		return stats, fmt.Errorf("синхронизация жанров: %w", err)
	}
	i.logger.Info("жанры синхронизированы", "count", len(genreSlugs))

	ids, err := i.collectIDs(ctx, opts)
	if err != nil {
		return stats, fmt.Errorf("сбор списка id: %w", err)
	}

	if !opts.DryRun && !opts.Refresh {
		existing, err := i.storage.ExistingFilmTMDBIDs(ctx, opts.Kind)
		if err != nil {
			return stats, fmt.Errorf("чтение уже загруженных id: %w", err)
		}
		pending := ids[:0:0]
		for _, id := range ids {
			if _, ok := existing[id]; ok {
				stats.Skipped++
				continue
			}
			pending = append(pending, id)
		}
		i.logger.Info("отсев уже загруженных", "было", len(ids), "осталось", len(pending))
		ids = pending
	}

	stats.Requested = int64(len(ids))
	if len(ids) == 0 {
		return stats, nil
	}

	var fetched, saved, failed, skipped atomic.Int64
	skipped.Store(stats.Skipped)

	idCh := make(chan int, 1000)
	results := make(chan PreparedFilm, 200)

	g, gCtx := errgroup.WithContext(ctx)

	g.Go(func() error {
		defer close(idCh)
		for _, id := range ids {
			select {
			case idCh <- id:
			case <-gCtx.Done():
				return gCtx.Err()
			}
		}
		return nil
	})

	var workersDone sync.WaitGroup
	for w := 0; w < opts.Workers; w++ {
		workersDone.Add(1)
		g.Go(func() error {
			defer workersDone.Done()
			for id := range idCh {
				prepared, err := i.fetch(gCtx, opts, id, genreSlugs)
				if err != nil {
					if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
						return err
					}
					if errors.Is(err, tmdb.ErrNotFound) {
						skipped.Add(1)
						i.logger.Debug("фильм удалён из TMDB", "kind", opts.Kind, "tmdb_id", id)
						continue
					}
					failed.Add(1)
					i.logger.Warn("не удалось загрузить фильм", "kind", opts.Kind, "tmdb_id", id, "error", err)
					continue
				}
				fetched.Add(1)
				select {
				case results <- prepared:
				case <-gCtx.Done():
					return gCtx.Err()
				}
			}
			return nil
		})
	}

	go func() {
		workersDone.Wait()
		close(results)
	}()

	g.Go(func() error {
		batch := make([]PreparedFilm, 0, opts.Batch)

		flush := func() error {
			if len(batch) == 0 {
				return nil
			}
			if !opts.DryRun {
				if err := i.storage.SaveFilms(gCtx, batch); err != nil {
					return err
				}
			}
			saved.Add(int64(len(batch)))
			i.logger.Info("пачка записана", "count", len(batch), "всего", saved.Load())
			batch = batch[:0]
			return nil
		}

		for prepared := range results {
			batch = append(batch, prepared)
			if len(batch) >= opts.Batch {
				if err := flush(); err != nil {
					return err
				}
			}
		}
		return flush()
	})

	err = g.Wait()

	stats.Fetched = fetched.Load()
	stats.Saved = saved.Load()
	stats.Failed = failed.Load()
	stats.Skipped = skipped.Load()

	return stats, err
}

func (i *Importer) syncGenres(ctx context.Context, kind film.Kind, dryRun bool) (map[int]string, error) {
	list := i.source.Genres
	if kind == film.KindSeries {
		list = i.source.TVGenres
	}
	ru, err := list(ctx, "ru-RU")
	if err != nil {
		return nil, err
	}
	en, err := list(ctx, "en-US")
	if err != nil {
		return nil, err
	}

	nameEn := make(map[int]string, len(en))
	for _, g := range en {
		nameEn[g.ID] = g.Name
	}

	genres := make([]film.Genre, 0, len(ru))
	slugs := make(map[int]string, len(ru))
	for _, g := range ru {
		english := nameEn[g.ID]
		if english == "" {
			english = g.Name
		}

		slug := slugify(english)
		if slug == "" {
			slug = fmt.Sprintf("genre-%d", g.ID)
		}
		slugs[g.ID] = slug
		genres = append(genres, film.Genre{
			ID:     slug,
			TMDBID: g.ID,
			NameRu: truncate(g.Name, 64),
			NameEn: truncate(english, 64),
		})
	}

	if dryRun {
		return slugs, nil
	}
	if kind == film.KindSeries {
		genres, err = i.newSeriesGenres(ctx, genres, slugs)
		if err != nil {
			return nil, err
		}
	}
	if err := i.storage.UpsertGenres(ctx, genres); err != nil {
		return nil, err
	}

	stored, err := i.storage.GenreSlugsByTMDBID(ctx)
	if err != nil {
		return nil, err
	}
	for tmdbID, slug := range stored {
		slugs[tmdbID] = slug
	}
	return slugs, nil
}

func (i *Importer) newSeriesGenres(ctx context.Context, genres []film.Genre, slugs map[int]string) ([]film.Genre, error) {
	stored, err := i.storage.GenreSlugsByTMDBID(ctx)
	if err != nil {
		return nil, err
	}
	taken := make(map[string]struct{}, len(stored))
	for _, slug := range stored {
		taken[slug] = struct{}{}
	}

	fresh := genres[:0:0]
	for _, genre := range genres {
		if _, ok := stored[genre.TMDBID]; ok {
			continue
		}
		if _, clash := taken[genre.ID]; clash {
			genre.ID += "-tv"
			slugs[genre.TMDBID] = genre.ID
		}
		fresh = append(fresh, genre)
	}
	return fresh, nil
}

const (
	tmdbMaxPage   = 500
	tmdbPageSize  = 20
	tmdbMaxWindow = tmdbMaxPage * tmdbPageSize
)

type window struct {
	from time.Time
	to   time.Time
}

func (i *Importer) collectIDs(ctx context.Context, opts Options) ([]int, error) {
	if len(opts.IDs) > 0 {
		return opts.IDs, nil
	}

	var (
		ids  []int
		seen = map[int]struct{}{}
	)

	for _, w := range yearWindows(opts.From, opts.To) {
		full, err := i.collectWindow(ctx, w, opts, seen, &ids, true)
		if err != nil {
			return nil, err
		}
		if full {
			return ids, nil
		}
	}
	return ids, nil
}

func (i *Importer) collectWindow(
	ctx context.Context,
	w window,
	opts Options,
	seen map[int]struct{},
	ids *[]int,
	allowSplit bool,
) (bool, error) {
	firstIDs, totalPages, totalResults, err := i.discover(ctx, opts, tmdb.DiscoverQuery{
		From: w.from, To: w.to, MinVotes: opts.MinVotes, Page: 1,
	})
	if err != nil {
		return false, err
	}

	if allowSplit && totalResults > tmdbMaxWindow {
		i.logger.Info("окно не помещается в пагинацию TMDB, режу на месяцы",
			"from", w.from.Format("2006-01-02"), "to", w.to.Format("2006-01-02"), "total", totalResults)
		for _, sub := range monthWindows(w) {
			full, err := i.collectWindow(ctx, sub, opts, seen, ids, false)
			if err != nil || full {
				return full, err
			}
		}
		return false, nil
	}

	lastPage := min(totalPages, tmdbMaxPage)

	for page := 1; page <= lastPage; page++ {
		pageIDs := firstIDs
		if page > 1 {
			pageIDs, _, _, err = i.discover(ctx, opts, tmdb.DiscoverQuery{
				From: w.from, To: w.to, MinVotes: opts.MinVotes, Page: page,
			})
			if err != nil {
				return false, err
			}
		}
		if len(pageIDs) == 0 {
			return false, nil
		}

		for _, id := range pageIDs {
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			*ids = append(*ids, id)
			if opts.Limit > 0 && len(*ids) >= opts.Limit {
				return true, nil
			}
		}
	}
	return false, nil
}

func yearWindows(from, to time.Time) []window {
	if from.IsZero() || to.IsZero() || to.Before(from) {
		return []window{{from: from, to: to}}
	}

	var windows []window
	for year := from.Year(); year <= to.Year(); year++ {
		start := time.Date(year, time.January, 1, 0, 0, 0, 0, time.UTC)
		end := time.Date(year, time.December, 31, 0, 0, 0, 0, time.UTC)
		if start.Before(from) {
			start = from
		}
		if end.After(to) {
			end = to
		}
		windows = append(windows, window{from: start, to: end})
	}
	return windows
}

func monthWindows(w window) []window {
	var windows []window
	cursor := time.Date(w.from.Year(), w.from.Month(), 1, 0, 0, 0, 0, time.UTC)

	for !cursor.After(w.to) {
		start := cursor
		if start.Before(w.from) {
			start = w.from
		}
		end := cursor.AddDate(0, 1, -1)
		if end.After(w.to) {
			end = w.to
		}
		windows = append(windows, window{from: start, to: end})
		cursor = cursor.AddDate(0, 1, 0)
	}
	return windows
}

func (i *Importer) prepare(movie *tmdb.Movie, genreSlugs map[int]string, topCast int) PreparedFilm {
	now := time.Now().UTC()

	titleEn, ok := movie.Translations.Title("en")
	if !ok {
		titleEn = movie.OriginalTitle
	}
	titleRu, ok := movie.Translations.Title("ru")
	if !ok {
		titleRu = movie.Title
	}
	if titleRu == "" {
		titleRu = movie.OriginalTitle
	}
	if titleEn == "" {
		titleEn = movie.OriginalTitle
	}

	synopsis := movie.Overview
	if synopsis == "" {
		if ru, ok := movie.Translations.Overview("ru"); ok {
			synopsis = ru
		} else if en, ok := movie.Translations.Overview("en"); ok {
			synopsis = en
		}
	}

	prepared := PreparedFilm{
		Adult: movie.Adult,
		Film: film.Film{
			TitleEn:         truncate(titleEn, 255),
			TitleRu:         truncate(titleRu, 255),
			Kind:            film.KindMovie,
			Synopsis:        truncate(synopsis, 2000),
			PosterURL:       truncate(i.source.PosterURL(movie.PosterPath), 512),
			IMDBID:          movie.IMDBID,
			TMDBID:          movie.ID,
			TMDBPopularity:  float32(movie.Popularity),
			Runtime:         movie.Runtime,
			TMDBVoteAverage: float32(movie.VoteAverage),
			TMDBVoteCount:   movie.VoteCount,
			TMDBPosterPath:  truncate(movie.PosterPath, 255),
			ReleaseDate:     parseReleaseDate(movie.ReleaseDate),

			SyncedAt: now,
		},
	}

	if c := movie.BelongsToCollection; c != nil && c.ID != 0 {
		id := c.ID
		prepared.Film.CollectionID = &id
		prepared.Collection = &film.Collection{
			TMDBID:     c.ID,
			Name:       truncate(c.Name, 255),
			PosterURL:  truncate(i.source.PosterURL(c.PosterPath), 512),
			PosterPath: truncate(c.PosterPath, 255),
		}
	}

	for _, g := range movie.Genres {
		if slug, ok := genreSlugs[g.ID]; ok {
			prepared.GenreSlugs = append(prepared.GenreSlugs, slug)
		}
	}

	credits := newCreditCollector(now)
	for idx, director := range movie.Credits.Directors() {
		credits.add(director.ID, director.Name, director.OriginalName, film.CreditRoleDirector, idx)
	}
	for _, actor := range movie.Credits.Cast {
		if actor.Order >= topCast {
			continue
		}
		credits.add(actor.ID, actor.Name, actor.OriginalName, film.CreditRoleActor, actor.Order)
	}
	prepared.Credits = credits.credits

	return prepared
}

type creditCollector struct {
	now     time.Time
	seen    map[creditKey]struct{}
	credits []PreparedCredit
}

func newCreditCollector(now time.Time) *creditCollector {
	return &creditCollector{now: now, seen: make(map[creditKey]struct{})}
}

func (c *creditCollector) add(personTMDBID int, name, originalName string, role film.CreditRole, order int) {
	if personTMDBID == 0 || name == "" {
		return
	}
	key := creditKey{personTMDBID: personTMDBID, role: role}
	if _, dup := c.seen[key]; dup {
		return
	}
	c.seen[key] = struct{}{}
	c.credits = append(c.credits, PreparedCredit{
		Person: film.Person{
			Name:         truncate(name, 255),
			OriginalName: truncate(originalName, 255),
			TMDBID:       personTMDBID,
			SyncedAt:     c.now,
		},
		Role:         role,
		BillingOrder: order,
	})
}

type creditKey struct {
	personTMDBID int
	role         film.CreditRole
}

func parseReleaseDate(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}
