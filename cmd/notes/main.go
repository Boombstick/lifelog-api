package main

import (
	"log/slog"
	"os"

	"lifelog-api/internal/config"
	"lifelog-api/internal/handler"
	"lifelog-api/internal/httpapi"
	"lifelog-api/internal/logging"
	"lifelog-api/internal/middleware"
	"lifelog-api/internal/note"
	"lifelog-api/internal/storage/postgres"
	"lifelog-api/internal/visits"
)

func run() error {
	logger := logging.Setup("notes")
	configuration, err := config.New()
	if err != nil {
		return err
	}

	database, err := postgres.NewNotesPostgres(postgres.ConfigFrom(configuration, configuration.NotesDatabase, logger))
	if err != nil {
		return err
	}

	service := note.NewService(postgres.NewNotesRepository(database))

	verifier := httpapi.Verifier(configuration, logger)

	checks, err := healthChecks(database, configuration)
	if err != nil {
		return err
	}

	engine := httpapi.NewEngine(httpapi.EngineOptions{Service: "notes", Logger: logger, Health: checks.Handler()})
	handler.NewNotesHandler(logger, service).
		Register(engine, middleware.AuthenticateAccount(verifier))

	recorder := visits.ForService(configuration, "notes", logger)
	return httpapi.Serve(httpapi.NewServer(configuration.BindHost+":"+configuration.NotesPort, visits.Wrap(engine, recorder)), logger,
		append(recorder.Jobs(), httpapi.WarmVerifier(verifier, logger))...)
}

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}
