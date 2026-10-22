package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"lifelog-api/internal/config"
	"lifelog-api/internal/handler"
	"lifelog-api/internal/httpapi"
	"lifelog-api/internal/logging"
	"lifelog-api/internal/storage/postgres"
	"lifelog-api/internal/trips"
	"lifelog-api/internal/trips/category"
	"lifelog-api/internal/trips/photostore"
	"lifelog-api/internal/trips/rates"
	"lifelog-api/internal/trips/seed"
	"lifelog-api/internal/trips/tz"
	"lifelog-api/internal/visits"

	"github.com/gin-gonic/gin"
)

const identityCacheTTL = 5 * time.Minute

const ratesHTTPTimeout = 30 * time.Second

func run() error {
	logger := logging.Setup("trips")
	configuration, err := config.New()
	if err != nil {
		return err
	}

	database, err := postgres.NewTripsPostgres(postgres.ConfigFrom(configuration, configuration.TripsDatabase, logger))
	if err != nil {
		return err
	}
	store := postgres.NewTripsStore(database)
	clock := trips.SystemClock{}

	zones, err := tz.New()
	if err != nil {
		return err
	}
	files, err := photostore.New(configuration.TripsPhotoPath)
	if err != nil {
		return err
	}
	categories, err := category.New(category.Options{
		Addresses: strings.Split(configuration.ElasticURL, ","),
		Username:  configuration.ElasticUsername,
		Password:  configuration.ElasticPassword,
		Index:     configuration.TripsCategoriesIndex,
	}, logger)
	if err != nil {
		return err
	}

	if configuration.TripsRatesAPIKey == "" {
		logger.Warn("TRIPS_RATES_API_KEY is empty, exchange rates will not refresh")
	}
	rateProvider := rates.NewProvider(store,
		rates.NewClient(configuration.TripsRatesBaseURL, configuration.TripsRatesAPIVersion,
			configuration.TripsRatesAPIKey, &http.Client{Timeout: ratesHTTPTimeout}),
		clock, logger)

	dictionary := trips.NewDictionaryService(store, categories, logger)
	tripService := trips.NewTripService(store, zones, files, clock, logger)
	expenses := trips.NewExpenseService(store, dictionary, categories, rateProvider, clock, logger)
	identityCache := trips.NewMemoryIdentityCache(identityCacheTTL)
	services := handler.TripsServices{
		Identity:   trips.NewIdentityService(store, identityCache, clock),
		Trips:      tripService,
		Expenses:   expenses,
		Reports:    trips.NewReportService(store, clock),
		Photos:     trips.NewPhotoService(store, files, clock),
		Dictionary: dictionary,
		Sync:       trips.NewSyncService(store, tripService, expenses, clock, logger),
		Merge:      trips.NewMergeService(store, identityCache, clock),
	}

	seedData, err := seed.Load()
	if err != nil {
		return err
	}
	seedCtx, cancelSeed := context.WithTimeout(context.Background(), time.Minute)
	err = dictionary.Seed(seedCtx, seedData)
	cancelSeed()
	if err != nil {
		return err
	}

	verifier := httpapi.Verifier(configuration, logger)

	checks, err := healthChecks(database, categories, store, configuration.AuthJWKSURL,
		configuration.TripsPhotoPath, configuration.TripsRatesAPIKey != "")
	if err != nil {
		return err
	}

	if configuration.AuthInternalToken == "" {
		logger.Warn("AUTH_INTERNAL_TOKEN is empty, the bot, merge and category writes will be rejected")
	}

	engine := httpapi.NewEngine(httpapi.EngineOptions{
		Service: "trips",
		Logger:  logger,
		Health:  checks.Handler(),

		MaxBodyBytes: 30 * 1000 * 1000,
		Recovery: func(c *gin.Context, _ any) {
			c.AbortWithStatusJSON(http.StatusInternalServerError, trips.ErrorResponse{Error: trips.MsgInternal})
		},
	})
	handler.NewTripsHandler(logger, services, verifier, configuration.AuthInternalToken).Register(engine)

	recorder := visits.ForService(configuration, "trips", logger)
	server := httpapi.NewServer(configuration.BindHost+":"+configuration.TripsPort, visits.Wrap(engine, recorder))

	server.ReadTimeout = 2 * time.Minute
	server.WriteTimeout = 2 * time.Minute

	ratesEvery := configuration.TripsRatesEvery
	if ratesEvery <= 0 {
		ratesEvery = 12 * time.Hour
	}
	return httpapi.Serve(server, logger, append(recorder.Jobs(), httpapi.WarmVerifier(verifier, logger), func(ctx context.Context) error {
		return rateProvider.Run(ctx, ratesEvery)
	})...)
}

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}
