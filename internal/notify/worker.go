package notify

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
)

type OutboxRepository interface {
	Claim(ctx context.Context, limit int, lease time.Duration) ([]Job, error)

	Complete(ctx context.Context, id int64, device uuid.UUID) error
	Remove(ctx context.Context, id int64) error
	Reschedule(ctx context.Context, id int64, attempts int, at time.Time) error

	RemoveDevice(ctx context.Context, target Target) error
	DeviceError(ctx context.Context, device uuid.UUID, text string) error
}

type Job struct {
	ID       int64
	Attempts int
	Payload  []byte
	Target   Target
}

const (
	batchSize   = 50
	concurrency = 8

	maxAttempts = 6
	baseBackoff = 30 * time.Second

	lease       = 5 * time.Minute
	passTimeout = 2 * time.Minute
	idlePeriod  = 5 * time.Second
)

type Worker struct {
	repo    OutboxRepository
	senders map[string]Sender
	logger  *slog.Logger
	wake    chan struct{}
	now     func() time.Time
}

func NewWorker(repo OutboxRepository, senders map[string]Sender, logger *slog.Logger) *Worker {
	return &Worker{
		repo:    repo,
		senders: senders,
		logger:  logger,
		wake:    make(chan struct{}, 1),
		now:     func() time.Time { return time.Now().UTC() },
	}
}

func (w *Worker) Wake() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *Worker) Run(ctx context.Context) error {
	for {
		claimed, err := w.Pass(ctx)
		if err != nil && ctx.Err() == nil {
			w.logger.Error("push outbox pass failed", "error", err)
		}

		if err == nil && claimed == batchSize {
			continue
		}
		select {
		case <-ctx.Done():
			return nil
		case <-w.wake:
		case <-time.After(idlePeriod):
		}
	}
}

func (w *Worker) Pass(ctx context.Context) (int, error) {
	passCtx, cancel := context.WithTimeout(ctx, passTimeout)
	defer cancel()

	jobs, err := w.repo.Claim(passCtx, batchSize, lease)
	if err != nil {
		return 0, err
	}
	group, groupCtx := errgroup.WithContext(passCtx)
	group.SetLimit(concurrency)
	for _, job := range jobs {
		group.Go(func() error {
			w.deliver(groupCtx, job)
			return nil
		})
	}
	return len(jobs), group.Wait()
}

func (w *Worker) deliver(ctx context.Context, job Job) {
	target := job.Target
	log := w.logger.With("outbox_id", job.ID, "device", target.ID, "user", target.UserID, "channel", target.Channel)

	var message Message
	if err := json.Unmarshal(job.Payload, &message); err != nil {
		log.Error("push payload unreadable, dropped", "error", err)
		w.remove(ctx, log, job.ID)
		return
	}
	if !w.now().Before(message.ExpiresAt) {
		deliveries.WithLabelValues(target.Channel, "expired").Inc()
		log.Info("push expired before delivery, dropped", "kind", message.Kind)
		w.remove(ctx, log, job.ID)
		return
	}
	sender := w.senders[target.Channel]
	if sender == nil {
		log.Warn("no sender for channel, dropped")
		w.remove(ctx, log, job.ID)
		return
	}

	err := sender.Send(ctx, target, message)

	if err != nil && ctx.Err() != nil {
		return
	}

	var retry *RetryableError
	switch {
	case err == nil, errors.Is(err, ErrLoggedOnly):
		outcome := "sent"
		if err != nil {
			outcome = "logged"
		}
		deliveries.WithLabelValues(target.Channel, outcome).Inc()
		if err := w.repo.Complete(ctx, job.ID, target.ID); err != nil {
			log.Error("push sent but outbox row not removed", "error", err)
		}

	case errors.Is(err, ErrTokenGone):
		deliveries.WithLabelValues(target.Channel, "gone").Inc()
		log.Info("push token no longer valid, removing device", "token", maskToken(target.Token), "error", err)
		if err := w.repo.RemoveDevice(ctx, target); err != nil {
			log.Error("device not removed", "error", err)
		}

	case errors.As(err, &retry):
		attempts := job.Attempts + 1
		if attempts >= maxAttempts {
			deliveries.WithLabelValues(target.Channel, "exhausted").Inc()
			log.Error("push dropped after last retry", "attempts", attempts, "error", err)
			w.noteDeviceError(ctx, log, target.ID, err)
			w.remove(ctx, log, job.ID)
			return
		}
		deliveries.WithLabelValues(target.Channel, "retry").Inc()
		delay := max(backoff(attempts), retry.After)
		log.Warn("push failed, will retry", "attempts", attempts, "in", delay.String(), "error", err)
		if err := w.repo.Reschedule(ctx, job.ID, attempts, w.now().Add(delay)); err != nil {
			log.Error("push not rescheduled", "error", err)
		}

	default:
		deliveries.WithLabelValues(target.Channel, "failed").Inc()
		log.Error("push failed permanently, dropped", "error", err)
		w.noteDeviceError(ctx, log, target.ID, err)
		w.remove(ctx, log, job.ID)
	}
}

func backoff(failures int) time.Duration {
	return baseBackoff << (failures - 1)
}

func (w *Worker) remove(ctx context.Context, log *slog.Logger, id int64) {
	if err := w.repo.Remove(ctx, id); err != nil {
		log.Error("outbox row not removed", "error", err)
	}
}

func (w *Worker) noteDeviceError(ctx context.Context, log *slog.Logger, device uuid.UUID, cause error) {
	if err := w.repo.DeviceError(ctx, device, cut(cause.Error(), 500)); err != nil {
		log.Error("device error not recorded", "error", err)
	}
}
