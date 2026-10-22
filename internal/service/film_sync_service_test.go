package service

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"lifelog-api/internal/importer"
)

type stubImporter struct {
	release chan struct{}
	started chan importer.Options
	stats   importer.Stats
	err     error
}

func newStubImporter() *stubImporter {
	return &stubImporter{
		release: make(chan struct{}),
		started: make(chan importer.Options, 1),
	}
}

func (s *stubImporter) Run(_ context.Context, opts importer.Options) (importer.Stats, error) {
	s.started <- opts
	<-s.release
	return s.stats, s.err
}

func newTestSyncService(catalogImporter CatalogImporter) *FilmSyncService {
	svc := NewFilmSyncService(catalogImporter, slog.New(slog.DiscardHandler))
	svc.now = func() time.Time { return time.Date(2026, time.September, 11, 12, 0, 0, 0, time.UTC) }
	return svc
}

func waitForState(t *testing.T, svc *FilmSyncService, want SyncState) SyncStatusResponse {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for {
		status := svc.Status()
		if status.State == want {
			return status
		}
		if time.Now().After(deadline) {
			t.Fatalf("состояние %q не наступило, текущее %q", want, status.State)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestSyncOptionsПереводитГодыВГраницыДат(t *testing.T) {
	t.Parallel()

	opts, err := syncOptions(StartSyncRequest{YearFrom: 2020, YearTo: 2022}, 2026)
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}

	wantFrom := time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC)
	wantTo := time.Date(2022, time.December, 31, 0, 0, 0, 0, time.UTC)
	if !opts.From.Equal(wantFrom) {
		t.Errorf("нижняя граница %v, ожидалась %v", opts.From, wantFrom)
	}
	if !opts.To.Equal(wantTo) {
		t.Errorf("верхняя граница %v, ожидалась %v", opts.To, wantTo)
	}

	if opts.Limit != 0 {
		t.Errorf("Limit = %d, ожидался 0", opts.Limit)
	}
	if opts.MinVotes != syncMinVotes {
		t.Errorf("MinVotes = %d, ожидался %d", opts.MinVotes, syncMinVotes)
	}
}

func TestSyncOptionsОдинГодДаётПолныйГод(t *testing.T) {
	t.Parallel()

	opts, err := syncOptions(StartSyncRequest{YearFrom: 2021, YearTo: 2021}, 2026)
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if opts.From.Month() != time.January || opts.From.Day() != 1 {
		t.Errorf("нижняя граница %v, ожидалось 1 января", opts.From)
	}
	if opts.To.Month() != time.December || opts.To.Day() != 31 {
		t.Errorf("верхняя граница %v, ожидалось 31 декабря", opts.To)
	}
}

func TestValidateSyncYears(t *testing.T) {
	t.Parallel()

	const currentYear = 2026

	tests := []struct {
		name    string
		from    int
		to      int
		wantErr bool
	}{
		{name: "обычный диапазон", from: 2016, to: 2026},
		{name: "один год", from: 2020, to: 2020},
		{name: "следующий год допустим: премьеры уже назначены", from: 2026, to: 2027},
		{name: "год начала позже года конца", from: 2022, to: 2020, wantErr: true},
		{name: "далёкое будущее", from: 2020, to: 2100, wantErr: true},
		{name: "до изобретения кино", from: 1800, to: 2000, wantErr: true},
		{name: "диапазон ровно по границе", from: 1967, to: 2026},
		{name: "диапазон шире предела", from: 1966, to: 2026, wantErr: true},
		{name: "с семидесятых по сегодня", from: 1970, to: 2026},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validateSyncYears(tt.from, tt.to, currentYear)
			if tt.wantErr {
				if !errors.Is(err, ErrSyncYears) {
					t.Fatalf("ожидалась ErrSyncYears, получено %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("неожиданная ошибка: %v", err)
			}
		})
	}
}

func TestStartОтбиваетВторойЗапускПокаИдётПервый(t *testing.T) {
	stub := newStubImporter()
	stub.stats = importer.Stats{Requested: 5, Saved: 5}
	svc := newTestSyncService(stub)

	first, err := svc.Start(StartSyncRequest{YearFrom: 2020, YearTo: 2021})
	if err != nil {
		t.Fatalf("первый запуск не удался: %v", err)
	}
	if first.State != SyncStateRunning || first.RunID == "" {
		t.Fatalf("ожидался запущенный прогон с идентификатором, получено %+v", first)
	}

	<-stub.started

	second, err := svc.Start(StartSyncRequest{YearFrom: 2020, YearTo: 2021})
	if !errors.Is(err, ErrSyncAlreadyRunning) {
		t.Fatalf("ожидалась ErrSyncAlreadyRunning, получено %v", err)
	}

	if second.RunID != first.RunID {
		t.Errorf("в отказе run_id = %q, ожидался %q", second.RunID, first.RunID)
	}

	close(stub.release)

	done := waitForState(t, svc, SyncStateSucceeded)
	if done.RunID != first.RunID {
		t.Errorf("после завершения run_id = %q, ожидался %q", done.RunID, first.RunID)
	}
	if done.Stats == nil || done.Stats.Saved != 5 {
		t.Fatalf("итоги прогона не сохранены: %+v", done.Stats)
	}

	if !done.IndexStale {
		t.Error("index_stale = false после записи новых фильмов")
	}
	if done.FinishedAt == nil {
		t.Error("время завершения не проставлено")
	}
}

func TestStartПослеЗавершенияРазрешёнСнова(t *testing.T) {
	stub := newStubImporter()
	svc := newTestSyncService(stub)

	if _, err := svc.Start(StartSyncRequest{YearFrom: 2020, YearTo: 2020}); err != nil {
		t.Fatalf("первый запуск не удался: %v", err)
	}
	<-stub.started
	close(stub.release)
	first := waitForState(t, svc, SyncStateSucceeded)

	second := newStubImporter()
	close(second.release)
	svc.importer = second

	status, err := svc.Start(StartSyncRequest{YearFrom: 2021, YearTo: 2021})
	if err != nil {
		t.Fatalf("повторный запуск не удался: %v", err)
	}
	if status.RunID == first.RunID {
		t.Error("новый прогон получил идентификатор предыдущего")
	}
	<-second.started
	waitForState(t, svc, SyncStateSucceeded)
}

func TestStartНеЗапускаетПрогонСНегоднымиГодами(t *testing.T) {
	t.Parallel()

	stub := newStubImporter()
	svc := newTestSyncService(stub)

	if _, err := svc.Start(StartSyncRequest{YearFrom: 2025, YearTo: 2020}); !errors.Is(err, ErrSyncYears) {
		t.Fatalf("ожидалась ErrSyncYears, получено %v", err)
	}
	if state := svc.Status().State; state != SyncStateIdle {
		t.Errorf("состояние %q, ожидалось %q", state, SyncStateIdle)
	}
	select {
	case opts := <-stub.started:
		t.Fatalf("импортёр был вызван с %+v", opts)
	default:
	}
}

func TestУпавшийИмпортОстаётсяВидимымВСтатусе(t *testing.T) {
	stub := newStubImporter()
	stub.err = errors.New("TMDB недоступен")
	stub.stats = importer.Stats{Requested: 10, Saved: 2, Failed: 8}
	close(stub.release)
	svc := newTestSyncService(stub)

	if _, err := svc.Start(StartSyncRequest{YearFrom: 2020, YearTo: 2020}); err != nil {
		t.Fatalf("запуск не удался: %v", err)
	}
	<-stub.started

	status := waitForState(t, svc, SyncStateFailed)
	if status.Error != "TMDB недоступен" {
		t.Errorf("текст ошибки %q", status.Error)
	}

	if !status.IndexStale {
		t.Error("index_stale = false, хотя часть карточек записана")
	}
	if status.Stats == nil || status.Stats.Failed != 8 {
		t.Fatalf("итоги упавшего прогона не сохранены: %+v", status.Stats)
	}
}

func TestБезКлючаTMDBСинхронизацияНедоступна(t *testing.T) {
	t.Parallel()

	svc := newTestSyncService(nil)

	if _, err := svc.Start(StartSyncRequest{YearFrom: 2020, YearTo: 2020}); !errors.Is(err, ErrSyncUnavailable) {
		t.Fatalf("ожидалась ErrSyncUnavailable, получено %v", err)
	}
}

func TestСтатусДоПервогоЗапуска(t *testing.T) {
	t.Parallel()

	status := newTestSyncService(newStubImporter()).Status()
	if status.State != SyncStateIdle {
		t.Errorf("состояние %q, ожидалось %q", status.State, SyncStateIdle)
	}
	if status.RunID != "" || status.StartedAt != nil || status.Stats != nil {
		t.Errorf("статус до запуска не пуст: %+v", status)
	}
}
