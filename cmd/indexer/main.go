package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"lifelog-api/internal/config"
	"lifelog-api/internal/indexer"
	"lifelog-api/internal/logging"
	"lifelog-api/internal/search"
	"lifelog-api/internal/storage/postgres"
)

func run() error {
	var (
		batch    = flag.Int("batch", 500, "сколько документов слать одним bulk-запросом")
		recreate = flag.Bool("recreate", false, "построить новое поколение индексов и переключить на него алиасы (нужно при смене маппинга)")
	)
	flag.Parse()

	logger := logging.Setup("indexer")

	configuration, err := config.New()
	if err != nil {
		return err
	}

	pgConfig := postgres.ConfigFrom(configuration, configuration.PostgresDatabase, logger)
	database, err := postgres.NewPostgres(pgConfig)
	if err != nil {
		return err
	}

	engine, err := search.NewClient(search.Options{
		Addresses:   strings.Split(configuration.ElasticURL, ","),
		Username:    configuration.ElasticUsername,
		Password:    configuration.ElasticPassword,
		IndexPrefix: configuration.ElasticPrefix,
		ReadTimeout: configuration.ElasticReadTimeout,
	})
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := engine.Ping(ctx); err != nil {
		return err
	}

	repo := postgres.NewIndexRepository(database)

	logger.Info("индексация начата", "batch", *batch, "recreate", *recreate,
		"films_index", engine.FilmsIndex(), "people_index", engine.PeopleIndex())

	stats, err := indexer.New(repo, engine, logger).Run(ctx, indexer.Options{
		Batch:    *batch,
		Recreate: *recreate,
	})
	if err != nil {
		logger.Error("индексация прервана", "films", stats.Films, "people", stats.People, "failed", stats.Failed, "error", err)
		return err
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		slog.Error("indexer", "error", err)
		os.Exit(1)
	}
}
