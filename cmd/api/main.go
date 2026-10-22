package main

import (
	"context"
	"lifelog-api/internal/config"
	"lifelog-api/internal/follows"
	"lifelog-api/internal/handler"
	"lifelog-api/internal/httpapi"
	"lifelog-api/internal/imdbimport"
	"lifelog-api/internal/importer"
	"lifelog-api/internal/indexer"
	"lifelog-api/internal/logging"
	"lifelog-api/internal/middleware"
	"lifelog-api/internal/router"
	"lifelog-api/internal/search"
	"lifelog-api/internal/service"
	"lifelog-api/internal/storage/postgres"
	"lifelog-api/internal/tmdb"
	"lifelog-api/internal/tmdbadd"
	"lifelog-api/internal/visits"
	"log/slog"
	"os"
	"strings"
	"time"

	_ "lifelog-api/docs"
)

func run() error {
	logger := logging.Setup("films")
	configuration, err := config.New()
	if err != nil {
		return err
	}
	pgConfig := postgres.ConfigFrom(configuration, configuration.PostgresDatabase, logger)
	database, err := postgres.NewPostgres(pgConfig)
	if err != nil {
		return err
	}
	placeRepo := postgres.ProvidePlaceRepository(database)
	placeService := service.NewPlaceService(placeRepo)
	placeHandler := handler.NewPlaceHandler(logger, placeService)

	userRepo := postgres.ProvideUserRepository(database)

	filmRepo := postgres.ProvideFilmRepository(database)

	var (
		ratingPublisher service.RatingPublisher
		catalogImporter service.CatalogImporter
		tmdbClient      *tmdb.Client

		importDeps = imdbimport.Deps{
			Catalog: filmRepo,
			Marks:   filmRepo,
			Store:   postgres.NewImportJobRepository(database),
		}

		addDeps = tmdbadd.Deps{Catalog: filmRepo}
	)
	if configuration.TMDBToken != "" {
		tmdbClient, err = tmdb.NewClient(tmdb.Options{
			BaseURL:       configuration.TMDBBaseUrl,
			Token:         configuration.TMDBToken,
			RPS:           configuration.TMDBRateLimitRPS(),
			Timeout:       configuration.TMDBTimeout,
			PosterBaseURL: configuration.TMDBPosterPath,
		})
		if err != nil {
			return err
		}
		ratingPublisher = tmdbClient
		cardImporter := importer.New(tmdbClient, filmRepo, logger)
		catalogImporter = cardImporter
		importDeps.Finder = tmdbClient
		importDeps.Fetcher = cardImporter
		importDeps.Localizer = importer.NewCardLocalizer(tmdbClient, filmRepo, logger)
		addDeps.Searcher = tmdbClient
		addDeps.Fetcher = cardImporter
		addDeps.Localizer = importDeps.Localizer
	} else {
		logger.Warn("TMDB_TOKEN не задан, оценки не будут уходить в TMDB")
	}

	authGraphs := follows.NewClient(configuration.FilmsAuthURL, configuration.AuthInternalToken, 3*time.Second)
	graphOptions := follows.Options{TTL: configuration.FilmsFollowsTTL, StaleFor: 10 * time.Minute}
	followGraph := follows.NewCache(authGraphs, graphOptions, logger)

	blockGraph := follows.NewBlockCache(authGraphs, graphOptions, logger)
	sharingRepo := postgres.NewSharingRepository(database)

	reviewGate := struct {
		*postgres.SharingRepository
		*follows.Cache
	}{sharingRepo, followGraph}
	filmService := service.NewFilmService(filmRepo, ratingPublisher, reviewGate, logger)

	searchEngine, err := search.NewClient(search.Options{
		Addresses:   strings.Split(configuration.ElasticURL, ","),
		Username:    configuration.ElasticUsername,
		Password:    configuration.ElasticPassword,
		IndexPrefix: configuration.ElasticPrefix,
		ReadTimeout: configuration.ElasticReadTimeout,
	})
	if err != nil {
		return err
	}

	pingCtx, cancelPing := context.WithTimeout(context.Background(), 5*time.Second)
	if err := searchEngine.Ping(pingCtx); err != nil {
		logger.Warn("Elasticsearch недоступен", "error", err)
	}
	cancelPing()
	searchService := service.NewSearchService(searchEngine, filmRepo, configuration.FilmsSearchCacheTTL)
	filmService.SetRatingIndex(service.FlushingRatingIndex(searchEngine, searchService))
	importDeps.Ratings = filmService
	searchHandler := handler.NewSearchHandler(logger, searchService)

	geoRepo := postgres.NewGeoRepository(database)
	geoService := service.NewGeoService(geoRepo)
	geoHandler := handler.NewGeoHandler(logger, geoService)
	filmHandler := handler.NewFilmHandler(logger, filmService, searchService, service.NewRecommendService(filmRepo, searchEngine))

	identityService := service.NewIdentityService(userRepo)

	personRepo := postgres.ProvidePersonRepository(database)
	personHandler := handler.NewPersonHandler(logger, service.NewPersonService(personRepo))

	sharingService := service.NewSharingService(sharingRepo, userRepo, filmService, followGraph, configuration.PublicURL)
	sharingHandler := handler.NewSharingHandler(logger, sharingService)
	sharedListService := service.NewSharedListService(postgres.NewSharedListRepository(database), followGraph, blockGraph, logger)
	sharedListHandler := handler.NewSharedListHandler(logger, sharedListService)

	internalHandler := handler.NewInternalHandler(logger, follows.Invalidators{followGraph, blockGraph}, sharedListService, filmRepo)

	importDeps.Indexer = service.FlushingCardIndexer(indexer.NewCardIndexer(postgres.NewIndexRepository(database), searchEngine, logger), searchService)
	addDeps.Indexer = importDeps.Indexer
	addDeps.Refresher = service.FlushingRefresher(searchEngine, searchService)
	tmdbAddHandler := handler.NewTMDBAddHandler(logger, tmdbadd.NewService(addDeps, logger))
	importService := imdbimport.NewService(importDeps, logger)
	if err := importService.Recover(context.Background()); err != nil {
		return err
	}

	defer importService.Shutdown(5 * time.Second)
	importHandler := handler.NewImportHandler(logger, importService)

	syncService := service.NewFilmSyncService(catalogImporter, logger)
	adminHandler := handler.NewAdminHandler(logger, syncService)

	verifier := httpapi.Verifier(configuration, logger)
	authMiddleware := middleware.Authenticate(verifier, identityService, logger)

	checks, err := healthChecks(database, searchEngine, configuration.AuthJWKSURL, tmdbClient, configuration.TMDBToken)
	if err != nil {
		return err
	}
	router := router.NewRouter(placeHandler, filmHandler, searchHandler, geoHandler, internalHandler, adminHandler, sharingHandler, personHandler, sharedListHandler, importHandler, tmdbAddHandler)
	httpHandler := router.SetupRoutes(
		checks.Handler(),
		authMiddleware,
		middleware.OptionalAuth(verifier, identityService),
		handler.RequireInternalToken(configuration.AuthInternalToken),
		middleware.RequireAdmin(),
	)
	recorder := visits.ForService(configuration, "films", logger)
	server := httpapi.NewServer(configuration.BindHost+":"+configuration.ServerPort, visits.Wrap(httpHandler, recorder))
	return httpapi.Serve(server, logger, append(recorder.Jobs(), httpapi.WarmVerifier(verifier, logger))...)
}

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}
