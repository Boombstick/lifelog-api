package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"

	"lifelog-api/internal/config"
	"lifelog-api/internal/notify"
)

func webPushSender(configuration *config.Config, logger *slog.Logger) (*notify.WebPushSender, *notify.VAPID, notify.EndpointGuard, error) {
	guard := notify.EndpointGuard{AllowTest: configuration.NotifyAllowTestEndpoints}
	public, private := configuration.NotifyVAPIDPublicKey, configuration.NotifyVAPIDPrivateKey
	if public == "" && private == "" {
		logger.Warn("NOTIFY_VAPID_* are empty, web push is off")
		return nil, nil, guard, nil
	}
	if public == "" || private == "" {
		return nil, nil, guard, errors.New("NOTIFY_VAPID_PUBLIC_KEY and NOTIFY_VAPID_PRIVATE_KEY must be set together")
	}
	vapid, err := notify.LoadVAPID(public, private, configuration.NotifyVAPIDSubject)
	if err != nil {
		return nil, nil, guard, err
	}
	if guard.AllowTest {
		logger.Warn("NOTIFY_ALLOW_TEST_ENDPOINTS is on: any push endpoint is accepted, never use this in prod")
	}
	logger.Info("web push configured", "subject", configuration.NotifyVAPIDSubject)
	return notify.NewWebPushSender(vapid, guard, configuration.PublicURL), vapid, guard, nil
}

func generateVAPIDKeys() error {
	public, private, err := notify.GenerateVAPIDKeys()
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(os.Stdout, "NOTIFY_VAPID_PUBLIC_KEY=%s\nNOTIFY_VAPID_PRIVATE_KEY=%s\n", public, private)
	return err
}
