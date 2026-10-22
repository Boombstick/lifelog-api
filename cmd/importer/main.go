package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"lifelog-api/internal/config"
	"lifelog-api/internal/film"
	"lifelog-api/internal/importer"
	"lifelog-api/internal/logging"
	"lifelog-api/internal/storage/postgres"
	"lifelog-api/internal/tmdb"
)

func run() error {
	var (
		limit     = flag.Int("limit", 10, "сколько фильмов загрузить (0 — все доступные страницы)")
		workers   = flag.Int("workers", 4, "размер пула загрузчиков")
		batch     = flag.Int("batch", 100, "сколько фильмов писать одной транзакцией")
		topCast   = flag.Int("top-cast", 10, "сколько актёров из начала титров сохранять")
		rps       = flag.Float64("rps", 0, "потолок запросов в секунду (0 — взять из конфига)")
		dryRun    = flag.Bool("dry-run", false, "сходить в TMDB, но ничего не писать в базу")
		refresh   = flag.Bool("refresh", false, "перекачать уже загруженные карточки, а не пропускать их")
		from      = flag.String("from", "", "нижняя граница даты релиза, YYYY-MM-DD")
		to        = flag.String("to", "", "верхняя граница даты релиза, YYYY-MM-DD")
		minVotes  = flag.Int("min-votes", 0, "минимум голосов на TMDB (отсекает короткометражки и любительское)")
		localize  = flag.String("localize", "", "вместо импорта добрать недостающее: films — русские названия, people — русские имена и портреты, all — и то и другое")
		kind      = flag.String("kind", "movie", "что импортировать: movie — фильмы, tv — сериалы")
		recommend = flag.Bool("recommend", false, "вместо импорта собрать списки похожих от TMDB")
		sync      = flag.Bool("sync", false, "вместо импорта обновить карточки, изменившиеся на TMDB (ежедневный прогон)")
		syncSince = flag.Duration("sync-since", 24*time.Hour, "как далеко назад смотреть изменения при --sync, максимум 336h")
	)
	flag.Parse()

	logger := logging.Setup("importer")

	configuration, err := config.New()
	if err != nil {
		return err
	}

	rateLimit := *rps
	if rateLimit <= 0 {
		rateLimit = configuration.TMDBRateLimitRPS()
	}

	client, err := tmdb.NewClient(tmdb.Options{
		BaseURL:       configuration.TMDBBaseUrl,
		Token:         configuration.TMDBToken,
		RPS:           rateLimit,
		Timeout:       configuration.TMDBTimeout,
		PosterBaseURL: configuration.TMDBPosterPath,
	})
	if err != nil {
		return err
	}

	pgConfig := postgres.ConfigFrom(configuration, configuration.PostgresDatabase, logger)
	database, err := postgres.NewPostgres(pgConfig)
	if err != nil {
		return err
	}

	repo := postgres.ProvideFilmRepository(database)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *recommend {
		return runRecommend(ctx, client, repo, logger, *workers, *dryRun)
	}
	if *localize != "" {
		return runLocalize(ctx, client, repo, logger, *localize, *workers, *dryRun)
	}

	importKind, err := parseKind(*kind)
	if err != nil {
		return err
	}

	if *sync {
		return runSync(ctx, client, repo, logger, importKind, syncOptions{
			since:   *syncSince,
			workers: *workers,
			batch:   *batch,
			topCast: *topCast,
			dryRun:  *dryRun,
		})
	}

	started := time.Now()
	logger.Info("импорт начат",
		"kind", importKind, "limit", *limit, "workers", *workers, "batch", *batch,
		"rps", rateLimit, "dry_run", *dryRun)

	fromDate, err := parseDate(*from)
	if err != nil {
		return fmt.Errorf("флаг --from: %w", err)
	}
	toDate, err := parseDate(*to)
	if err != nil {
		return fmt.Errorf("флаг --to: %w", err)
	}

	stats, err := importer.New(client, repo, logger).Run(ctx, importer.Options{
		Kind:     importKind,
		From:     fromDate,
		To:       toDate,
		MinVotes: *minVotes,
		Limit:    *limit,
		Workers:  *workers,
		Batch:    *batch,
		TopCast:  *topCast,
		DryRun:   *dryRun,
		Refresh:  *refresh,
	})

	logger.Info("импорт завершён",
		"requested", stats.Requested,
		"fetched", stats.Fetched,
		"saved", stats.Saved,
		"skipped", stats.Skipped,
		"failed", stats.Failed,
		"elapsed", time.Since(started).Round(time.Millisecond))

	if errors.Is(err, context.Canceled) {
		logger.Info("остановлено по сигналу")
		return nil
	}
	return err
}

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func parseKind(value string) (film.Kind, error) {
	switch value {
	case "movie":
		return film.KindMovie, nil
	case "tv":
		return film.KindSeries, nil
	}
	return "", fmt.Errorf("флаг --kind: ожидалось movie или tv, получено %q", value)
}

func parseDate(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	return time.Parse("2006-01-02", value)
}

func runLocalize(
	ctx context.Context,
	client *tmdb.Client,
	repo *postgres.FilmRepository,
	logger *slog.Logger,
	mode string,
	workers int,
	dryRun bool,
) error {
	opts := importer.LocalizeOptions{Workers: workers, DryRun: dryRun}
	switch mode {
	case "films":
		opts.Films = true
	case "people":
		opts.People = true
	case "all":
		opts.Films, opts.People = true, true
	default:
		return fmt.Errorf("флаг --localize: ожидалось films, people или all, получено %q", mode)
	}

	started := time.Now()
	logger.Info("локализация начата", "режим", mode, "workers", workers, "dry_run", dryRun)

	stats, err := importer.NewLocalizer(client, repo, logger).Run(ctx, opts)
	logger.Info("локализация завершена",
		"фильмов проверено", stats.FilmsChecked, "названий добавлено", stats.FilmsFilled,
		"людей проверено", stats.PeopleChecked, "людей дополнено", stats.PeopleFilled,
		"ошибок", stats.Errors, "elapsed", time.Since(started))
	if err != nil {
		return err
	}

	logger.Info("не забудьте переиндексировать: go run ./cmd/indexer --recreate")
	return nil
}

func runRecommend(
	ctx context.Context,
	client *tmdb.Client,
	repo *postgres.FilmRepository,
	logger *slog.Logger,
	workers int,
	dryRun bool,
) error {
	started := time.Now()
	logger.Info("сбор рекомендаций начат", "workers", workers, "dry_run", dryRun)

	stats, err := importer.NewRecommender(client, repo, logger).Run(ctx, workers, dryRun)
	logger.Info("сбор рекомендаций завершён",
		"фильмов", stats.Checked, "связей", stats.Linked, "ошибок", stats.Errors,
		"elapsed", time.Since(started))
	return err
}

type syncOptions struct {
	since   time.Duration
	workers int
	batch   int
	topCast int
	dryRun  bool
}

func runSync(
	ctx context.Context,
	client *tmdb.Client,
	repo *postgres.FilmRepository,
	logger *slog.Logger,
	kind film.Kind,
	opts syncOptions,
) error {
	started := time.Now()
	logger.Info("синхронизация изменений начата",
		"kind", kind, "since", opts.since, "workers", opts.workers, "dry_run", opts.dryRun)

	syncer := importer.NewSyncer(client, importer.New(client, repo, logger), repo, logger)
	stats, err := syncer.Run(ctx, importer.SyncOptions{
		Kind:    kind,
		Since:   opts.since,
		Workers: opts.workers,
		Batch:   opts.batch,
		TopCast: opts.topCast,
		DryRun:  opts.dryRun,
	})

	logger.Info("синхронизация изменений завершена",
		"kind", kind,
		"изменилось_на_tmdb", stats.Changed,
		"наших", stats.Ours,
		"обновлено", stats.Import.Saved,
		"не_удалось", stats.Import.Failed,
		"elapsed", time.Since(started).Round(time.Millisecond))

	if errors.Is(err, context.Canceled) {
		logger.Info("остановлено по сигналу")
		return nil
	}
	return err
}
