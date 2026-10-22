package main

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"lifelog-api/internal/calendar"
	"lifelog-api/internal/config"
	"lifelog-api/internal/handler"
	"lifelog-api/internal/httpapi"
	"lifelog-api/internal/logging"
	"lifelog-api/internal/middleware"
	"lifelog-api/internal/storage/postgres"
	"lifelog-api/internal/visits"
)

func run() error {
	logger := logging.Setup("calendar")
	configuration, err := config.New()
	if err != nil {
		return err
	}

	database, err := postgres.NewCalendarPostgres(postgres.ConfigFrom(configuration, configuration.CalendarDatabase, logger))
	if err != nil {
		return err
	}

	repo := postgres.NewCalendarRepository(database)

	var catalog calendar.CatalogSource
	addresses := calendar.NewAuthClient(configuration.CalendarAuthURL, configuration.AuthInternalToken)

	var messenger calendar.Messenger = calendar.LogMessenger{Logger: logger}
	var telegram *calendar.TelegramMessenger
	if configuration.TelegramBotToken != "" {
		telegram = calendar.NewTelegramMessenger(
			configuration.TelegramBotToken,
			configuration.TelegramAPIBaseURL,
			configuration.TelegramRelaySecret,
		)
		messenger = telegram
	} else {
		logger.Warn("TELEGRAM_BOT_TOKEN is empty, notifications will only be logged")
	}

	var pusher calendar.Pusher
	var notifyClient *calendar.NotifyClient
	if configuration.NotifyURL != "" {
		notifyClient = calendar.NewNotifyClient(configuration.NotifyURL, configuration.AuthInternalToken)
		pusher = notifyClient
	} else {
		logger.Warn("NOTIFY_URL is empty, push notifications are off")
	}

	service := calendar.NewService(repo, catalog, logger)
	notifier := calendar.NewNotifier(repo, catalog, addresses, messenger, pusher, logger)

	var google *calendar.GoogleImport
	if configuration.CalendarGoogleClientID != "" {
		sealer, err := calendar.NewSealer(configuration.CalendarGoogleTokenKey)
		if err != nil {
			return err
		}
		redirect := configuration.CalendarGoogleRedirectURL
		if redirect == "" {
			redirect = strings.TrimRight(configuration.PublicURL, "/") + "/calendar/google/callback"
		}
		google = calendar.NewGoogleImport(repo, calendar.NewGoogleClient(calendar.GoogleConfig{
			ClientID:     configuration.CalendarGoogleClientID,
			ClientSecret: configuration.CalendarGoogleClientSecret,
			RedirectURL:  redirect,
		}), sealer, logger)
	} else {
		logger.Warn("CALENDAR_GOOGLE_CLIENT_ID is empty, google import is off")
	}

	verifier := httpapi.Verifier(configuration, logger)

	checks, err := healthChecks(database, addresses, telegram, notifyClient, configuration)
	if err != nil {
		return err
	}

	engine := httpapi.NewEngine(httpapi.EngineOptions{Service: "calendar", Logger: logger, Health: checks.Handler()})
	handler.NewCalendarHandler(logger, service, google).
		Register(engine, middleware.AuthenticateAccount(verifier))

	tick := configuration.CalendarTick
	if tick <= 0 {
		tick = time.Minute
	}

	jobs := []func(context.Context) error{func(ctx context.Context) error {
		return httpapi.Every(ctx, tick, logger, "notification pass failed", notifier.Run)
	}}
	if google != nil {
		every := configuration.CalendarGoogleSyncEvery
		if every <= 0 {
			every = 30 * time.Minute
		}
		jobs = append(jobs, func(ctx context.Context) error {
			return httpapi.Every(ctx, every, logger, "google sync pass failed", google.SyncAll)
		})
	}

	recorder := visits.ForService(configuration, "calendar", logger)
	jobs = append(jobs, recorder.Jobs()...)
	return httpapi.Serve(httpapi.NewServer(configuration.BindHost+":"+configuration.CalendarPort, visits.Wrap(engine, recorder)), logger, append(jobs, httpapi.WarmVerifier(verifier, logger))...)
}

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}
