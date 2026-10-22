package notify

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

type Sender interface {
	Send(ctx context.Context, target Target, message Message) error
}

var ErrTokenGone = errors.New("push token is no longer registered")

var ErrLoggedOnly = errors.New("push logged, not sent")

type RetryableError struct {
	Err   error
	After time.Duration
}

func (e *RetryableError) Error() string { return "retryable: " + e.Err.Error() }
func (e *RetryableError) Unwrap() error { return e.Err }

func retryable(err error, after time.Duration) error {
	return &RetryableError{Err: err, After: after}
}

type LogSender struct {
	Logger *slog.Logger
}

func (s LogSender) Send(_ context.Context, target Target, message Message) error {
	s.Logger.Info("push not sent, no FCM credentials",
		"user", target.UserID, "device", target.ID, "token", maskToken(target.Token),
		"kind", message.Kind, "title_len", len([]rune(message.Title)), "body_len", len([]rune(message.Body)))
	return ErrLoggedOnly
}
