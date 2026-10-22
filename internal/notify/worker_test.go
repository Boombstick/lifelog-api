package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

type fakeOutbox struct {
	mu          sync.Mutex
	jobs        []Job
	completed   []int64
	removed     []int64
	rescheduled map[int64]rescheduled
	devicesGone []uuid.UUID
	goneTargets []Target
	deviceErrs  map[uuid.UUID]string
}

type rescheduled struct {
	attempts int
	at       time.Time
}

func newFakeOutbox(jobs ...Job) *fakeOutbox {
	return &fakeOutbox{jobs: jobs, rescheduled: map[int64]rescheduled{}, deviceErrs: map[uuid.UUID]string{}}
}

func (f *fakeOutbox) Claim(_ context.Context, limit int, _ time.Duration) ([]Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := min(limit, len(f.jobs))
	out := f.jobs[:n]
	f.jobs = f.jobs[n:]
	return out, nil
}
func (f *fakeOutbox) Complete(_ context.Context, id int64, _ uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.completed = append(f.completed, id)
	return nil
}
func (f *fakeOutbox) Remove(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = append(f.removed, id)
	return nil
}
func (f *fakeOutbox) Reschedule(_ context.Context, id int64, attempts int, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rescheduled[id] = rescheduled{attempts: attempts, at: at}
	return nil
}
func (f *fakeOutbox) RemoveDevice(_ context.Context, target Target) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.devicesGone = append(f.devicesGone, target.ID)
	f.goneTargets = append(f.goneTargets, target)
	return nil
}
func (f *fakeOutbox) DeviceError(_ context.Context, device uuid.UUID, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deviceErrs[device] = text
	return nil
}

type scriptedSender struct {
	err  error
	sent int
}

func (s *scriptedSender) Send(context.Context, Target, Message) error {
	s.sent++
	return s.err
}

var workerNow = time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func newTestWorker(repo OutboxRepository, sender Sender) *Worker {
	w := NewWorker(repo, map[string]Sender{ChannelFCM: sender}, quietLogger())
	w.now = func() time.Time { return workerNow }
	return w
}

func job(t *testing.T, id int64, attempts int, message Message) Job {
	t.Helper()
	payload, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	return Job{ID: id, Attempts: attempts, Payload: payload, Target: testTarget()}
}

func liveMessage() Message {
	m := testMessage(KindReminder)
	m.ExpiresAt = workerNow.Add(time.Hour)
	return m
}

func TestWorkerSuccessCompletes(t *testing.T) {
	t.Parallel()
	repo := newFakeOutbox(job(t, 1, 0, liveMessage()))
	sender := &scriptedSender{}
	if _, err := newTestWorker(repo, sender).Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	if sender.sent != 1 || len(repo.completed) != 1 || repo.completed[0] != 1 {
		t.Fatalf("sent=%d completed=%v", sender.sent, repo.completed)
	}
}

func TestWorkerDeletesDeviceOnUnregistered(t *testing.T) {
	t.Parallel()
	fake := &fakeFCM{answers: []fakeAnswer{{status: 404, body: `{"error":{"code":404,"status":"NOT_FOUND","details":[{"errorCode":"UNREGISTERED"}]}}`}}}
	sender := newTestSender(t, fake)
	sender.now = func() time.Time { return workerNow }

	queued := job(t, 7, 0, liveMessage())
	repo := newFakeOutbox(queued)
	if _, err := newTestWorker(repo, sender).Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(repo.devicesGone) != 1 || repo.devicesGone[0] != queued.Target.ID {
		t.Fatalf("device not removed: %v", repo.devicesGone)
	}

	if got := repo.goneTargets[0]; got.Token != queued.Target.Token || got.Endpoint != queued.Target.Endpoint {
		t.Errorf("removal not tied to the failed token: %+v", got)
	}
	if len(repo.completed) != 0 || len(repo.rescheduled) != 0 {
		t.Errorf("nothing else should happen: completed=%v rescheduled=%v", repo.completed, repo.rescheduled)
	}
}

func TestWorkerRetriesServerErrorsWithBackoff(t *testing.T) {
	t.Parallel()
	fake := &fakeFCM{answers: []fakeAnswer{
		{status: 503, body: `{"error":{"code":503,"status":"UNAVAILABLE"}}`},
		{status: 500, body: `{}`},
	}}
	sender := newTestSender(t, fake)
	sender.now = func() time.Time { return workerNow }

	firstRepo := newFakeOutbox(job(t, 1, 0, liveMessage()))
	if _, err := newTestWorker(firstRepo, sender).Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	secondRepo := newFakeOutbox(job(t, 2, 3, liveMessage()))
	if _, err := newTestWorker(secondRepo, sender).Pass(context.Background()); err != nil {
		t.Fatal(err)
	}

	first, second := firstRepo.rescheduled[1], secondRepo.rescheduled[2]
	if first.attempts != 1 || !first.at.Equal(workerNow.Add(30*time.Second)) {
		t.Errorf("first failure: %+v", first)
	}
	if second.attempts != 4 || !second.at.Equal(workerNow.Add(4*time.Minute)) {
		t.Errorf("fourth failure: %+v", second)
	}
}

func TestWorkerHonoursRetryAfter(t *testing.T) {
	t.Parallel()
	repo := newFakeOutbox(job(t, 1, 0, liveMessage()))
	sender := &scriptedSender{err: retryable(errors.New("429"), 10*time.Minute)}
	if _, err := newTestWorker(repo, sender).Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := repo.rescheduled[1]; !got.at.Equal(workerNow.Add(10 * time.Minute)) {
		t.Errorf("Retry-After ignored: %+v", got)
	}
}

func TestWorkerDropsAfterLastAttempt(t *testing.T) {
	t.Parallel()
	queued := job(t, 1, maxAttempts-1, liveMessage())
	repo := newFakeOutbox(queued)
	sender := &scriptedSender{err: retryable(errors.New("503"), 0)}
	if _, err := newTestWorker(repo, sender).Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(repo.removed) != 1 || len(repo.rescheduled) != 0 {
		t.Fatalf("removed=%v rescheduled=%v", repo.removed, repo.rescheduled)
	}
	if repo.deviceErrs[queued.Target.ID] == "" {
		t.Errorf("the reason should land on the device")
	}
}

func TestWorkerPermanentErrorKeepsDevice(t *testing.T) {
	t.Parallel()
	queued := job(t, 1, 0, liveMessage())
	repo := newFakeOutbox(queued)
	sender := &scriptedSender{err: errors.New("fcm answered 401 UNAUTHENTICATED")}
	if _, err := newTestWorker(repo, sender).Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(repo.devicesGone) != 0 {
		t.Errorf("a config error must not delete the device")
	}
	if len(repo.removed) != 1 || repo.deviceErrs[queued.Target.ID] == "" {
		t.Errorf("removed=%v errs=%v", repo.removed, repo.deviceErrs)
	}
}

func TestWorkerDropsExpired(t *testing.T) {
	t.Parallel()
	message := liveMessage()
	message.ExpiresAt = workerNow.Add(-time.Second)
	repo := newFakeOutbox(job(t, 1, 2, message))
	sender := &scriptedSender{}
	if _, err := newTestWorker(repo, sender).Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	if sender.sent != 0 || len(repo.removed) != 1 {
		t.Fatalf("expired push must be dropped unsent: sent=%d removed=%v", sender.sent, repo.removed)
	}
}

func TestWorkerDropsAtExactDeadline(t *testing.T) {
	t.Parallel()
	message := liveMessage()
	message.ExpiresAt = workerNow
	repo := newFakeOutbox(job(t, 1, 0, message))
	sender := &scriptedSender{}
	if _, err := newTestWorker(repo, sender).Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	if sender.sent != 0 || len(repo.removed) != 1 {
		t.Fatalf("push due exactly now must be dropped: sent=%d removed=%v", sender.sent, repo.removed)
	}
}

func TestWorkerRemovesUndeliverableRows(t *testing.T) {
	t.Parallel()
	noSender := job(t, 1, 0, liveMessage())
	noSender.Target.Channel = "sms"
	garbage := job(t, 2, 0, liveMessage())
	garbage.Payload = []byte("{not json")

	repo := newFakeOutbox(noSender, garbage)
	sender := &scriptedSender{}
	if _, err := newTestWorker(repo, sender).Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	slices.Sort(repo.removed)
	if sender.sent != 0 || !slices.Equal(repo.removed, []int64{1, 2}) {
		t.Fatalf("sent=%d removed=%v, want both rows removed unsent", sender.sent, repo.removed)
	}
}

type cancellingSender struct{ cancel context.CancelFunc }

func (s cancellingSender) Send(context.Context, Target, Message) error {
	s.cancel()
	return errors.New("context canceled mid-send")
}

func TestWorkerLeavesRowOnShutdown(t *testing.T) {
	t.Parallel()
	queued := job(t, 1, 0, liveMessage())
	repo := newFakeOutbox(queued)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := newTestWorker(repo, cancellingSender{cancel: cancel}).Pass(ctx); err != nil {
		t.Fatal(err)
	}
	if len(repo.removed) != 0 || len(repo.rescheduled) != 0 || len(repo.deviceErrs) != 0 {
		t.Fatalf("shutdown must leave the row for the lease: removed=%v rescheduled=%v errs=%v",
			repo.removed, repo.rescheduled, repo.deviceErrs)
	}
}

func TestLogSenderLogsNoSecrets(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	sender := LogSender{Logger: slog.New(slog.NewJSONHandler(&out, nil))}
	target := testTarget()
	message := testMessage(KindReminder)
	if err := sender.Send(context.Background(), target, message); !errors.Is(err, ErrLoggedOnly) {
		t.Fatalf("LogSender answered %v, want ErrLoggedOnly", err)
	}
	logged := out.String()
	if strings.Contains(logged, target.Token) || strings.Contains(logged, message.Title) || strings.Contains(logged, message.Body) {
		t.Fatalf("log line carries the token or the text: %s", logged)
	}
}

func counterValue(t *testing.T, counter prometheus.Counter) float64 {
	t.Helper()
	var metric dto.Metric
	if err := counter.Write(&metric); err != nil {
		t.Fatal(err)
	}
	return metric.GetCounter().GetValue()
}

func TestWorkerCompletesLoggedOnlyPush(t *testing.T) {
	t.Parallel()
	queued := job(t, 1, 0, liveMessage())
	repo := newFakeOutbox(queued)

	logged := deliveries.WithLabelValues(ChannelFCM, "logged")
	before := counterValue(t, logged)
	if _, err := newTestWorker(repo, LogSender{Logger: quietLogger()}).Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(repo.completed) != 1 || len(repo.removed) != 0 || len(repo.deviceErrs) != 0 {
		t.Fatalf("completed=%v removed=%v errs=%v", repo.completed, repo.removed, repo.deviceErrs)
	}
	if got := counterValue(t, logged) - before; got != 1 {
		t.Fatalf("logged outcome counted %v times, want 1", got)
	}
}

func TestBackoff(t *testing.T) {
	t.Parallel()
	want := []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute}
	for i, w := range want {
		if got := backoff(i + 1); got != w {
			t.Errorf("backoff(%d) = %v, want %v", i+1, got, w)
		}
	}
}
