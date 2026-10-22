package main

import (
	"errors"
	"flag"
	"log/slog"
	"os"

	"lifelog-api/internal/config"
	"lifelog-api/internal/handler"
	"lifelog-api/internal/httpapi"
	"lifelog-api/internal/logging"
	"lifelog-api/internal/middleware"
	"lifelog-api/internal/notify"
	"lifelog-api/internal/storage/postgres"
	"lifelog-api/internal/visits"
)

func run() error {
	logger := logging.Setup("notify")
	configuration, err := config.New()
	if err != nil {
		return err
	}

	database, err := postgres.NewNotifyPostgres(postgres.ConfigFrom(configuration, configuration.NotifyDatabase, logger))
	if err != nil {
		return err
	}
	repo := postgres.NewNotifyRepository(database)

	fcm, err := fcmSender(configuration, logger)
	if err != nil {
		return err
	}
	var sender notify.Sender = notify.LogSender{Logger: logger}
	if fcm != nil {
		sender = fcm
	}

	webPush, vapid, guard, err := webPushSender(configuration, logger)
	if err != nil {
		return err
	}
	senders := map[string]notify.Sender{notify.ChannelFCM: sender}
	if webPush != nil {
		senders[notify.ChannelWebPush] = webPush
	}

	worker := notify.NewWorker(repo, senders, logger)
	service := notify.NewService(repo, worker.Wake)
	if vapid != nil {
		service.EnableWebPush(vapid.PublicKey(), guard)
	}

	verifier := httpapi.Verifier(configuration, logger)

	checks, err := healthChecks(database, fcm, webPush != nil, configuration)
	if err != nil {
		return err
	}

	engine := httpapi.NewEngine(httpapi.EngineOptions{Service: "notify", Logger: logger, Health: checks.Handler()})
	handler.NewNotifyHandler(logger, service).Register(engine,
		middleware.AuthenticateAccount(verifier),
		handler.RequireInternalToken(configuration.AuthInternalToken))

	recorder := visits.ForService(configuration, "notify", logger)
	return httpapi.Serve(httpapi.NewServer(configuration.BindHost+":"+configuration.NotifyPort, visits.Wrap(engine, recorder)), logger,
		append(recorder.Jobs(), worker.Run, httpapi.WarmVerifier(verifier, logger))...)
}

func fcmSender(configuration *config.Config, logger *slog.Logger) (*notify.FCMSender, error) {
	path := configuration.NotifyFCMCredentialsFile
	if path == "" {
		logger.Warn("NOTIFY_FCM_CREDENTIALS_FILE is empty, pushes will only be logged")
		return nil, nil
	}
	sender, err := notify.LoadFCMSender(path, configuration.NotifyFCMEndpoint)
	if errors.Is(err, notify.ErrNoCredentials) {
		logger.Warn("FCM credentials file not found, pushes will only be logged", "path", path)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	logger.Info("FCM sender configured", "project", sender.ProjectID())
	return sender, nil
}

func main() {
	generate := flag.Bool("generate-vapid-keys", false, "print a new VAPID key pair as env lines and exit")
	flag.Parse()
	if *generate {
		if err := generateVAPIDKeys(); err != nil {
			slog.Error("fatal", "error", err)
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}
