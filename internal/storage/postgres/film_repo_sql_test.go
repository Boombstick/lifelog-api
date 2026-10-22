package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"lifelog-api/internal/film"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var errNoRows = errors.New("recordingPool answers no queries")

type recordingPool struct {
	mu         sync.Mutex
	statements []string
	args       [][]any
}

func (p *recordingPool) record(query string, args []any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.statements = append(p.statements, query)
	p.args = append(p.args, args)
}

func (p *recordingPool) PrepareContext(context.Context, string) (*sql.Stmt, error) {
	return nil, errNoRows
}

func (p *recordingPool) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	p.record(query, args)
	return driver.RowsAffected(1), nil
}

func (p *recordingPool) QueryContext(_ context.Context, query string, args ...any) (*sql.Rows, error) {
	p.record(query, args)
	return nil, errNoRows
}

func (p *recordingPool) QueryRowContext(_ context.Context, query string, args ...any) *sql.Row {
	p.record(query, args)
	return nil
}

func (p *recordingPool) BeginTx(context.Context, *sql.TxOptions) (gorm.ConnPool, error) {
	return &recordingTx{p}, nil
}

type recordingTx struct{ *recordingPool }

func (*recordingTx) Commit() error   { return nil }
func (*recordingTx) Rollback() error { return nil }

func newRecordingRepo(t *testing.T) (*FilmRepository, *recordingPool) {
	t.Helper()
	pool := &recordingPool{}
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{
		DisableAutomaticPing: true,
		Logger:               logger.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	return ProvideFilmRepository(db), pool
}

func isNull(arg any) bool {
	if arg == nil {
		return true
	}
	value := reflect.ValueOf(arg)
	return value.Kind() == reflect.Pointer && value.IsNil()
}

func isFilmLock(statement string) bool {
	return strings.Contains(statement, "FROM films WHERE id =") && strings.Contains(statement, "FOR UPDATE")
}

func TestRatingTransactionsLockFilmFirst(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rate := int16(8)
	watching := film.SeasonStateWatching

	cases := []struct {
		name string
		call func(*FilmRepository) error
	}{
		{"rating", func(r *FilmRepository) error {
			return r.AddRateAndDescription(ctx, 7, 1, &rate, nil)
		}},
		{"imported rating", func(r *FilmRepository) error {
			return r.RateWatchedAt(ctx, 7, 1, rate, time.Now())
		}},
		{"removed review", func(r *FilmRepository) error {
			return r.RemoveReview(ctx, 7, 1)
		}},
		{"season removed", func(r *FilmRepository) error {
			return r.SetSeasonEntry(ctx, 1, 7, film.Season{ID: 3}, nil, nil, false, time.Now())
		}},
		{"season state", func(r *FilmRepository) error {
			return r.SetSeasonEntry(ctx, 1, 7, film.Season{ID: 3}, &watching, nil, false, time.Now())
		}},
		{"seasons marked", func(r *FilmRepository) error {
			return r.MarkSeasonsWatched(ctx, 1, 7, []uint{3, 4}, time.Now())
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, pool := newRecordingRepo(t)
			_ = tc.call(repo)
			if len(pool.statements) < 2 || !isFilmLock(pool.statements[0]) {
				t.Fatalf("the film lock must come first, got %q", pool.statements)
			}
			for _, statement := range pool.statements[1:] {
				if isFilmLock(statement) {
					t.Errorf("locked twice: %q", pool.statements)
				}
			}
		})
	}
}

func TestTextOnlyReviewTakesNoLock(t *testing.T) {
	t.Parallel()
	repo, pool := newRecordingRepo(t)
	text := "Долго, но красиво"

	_ = repo.AddRateAndDescription(context.Background(), 7, 1, nil, &text)
	for _, statement := range pool.statements {
		if isFilmLock(statement) {
			t.Fatalf("text only must not lock the film: %q", pool.statements)
		}
	}
}

func TestEmptyDescriptionOnlyClearsText(t *testing.T) {
	t.Parallel()

	for _, text := range []string{"", "   "} {
		repo, pool := newRecordingRepo(t)
		if err := repo.AddRateAndDescription(context.Background(), 7, 1, nil, &text); err != nil {
			t.Fatal(err)
		}
		if len(pool.statements) != 1 {
			t.Fatalf("%q: want one statement, got %q", text, pool.statements)
		}
		statement := pool.statements[0]
		if !strings.HasPrefix(statement, `UPDATE "film_entries" SET "description"=$1`) ||
			strings.Contains(statement, "watched_at") || strings.Contains(statement, "watchlisted") {
			t.Errorf("%q: want an update of the text alone, got %q", text, statement)
		}
		if !isNull(pool.args[0][0]) {
			t.Errorf("%q: want the text set to NULL, got %v", text, pool.args[0][0])
		}
	}
}

func TestEmptyDescriptionWithRateStoresNoText(t *testing.T) {
	t.Parallel()
	repo, pool := newRecordingRepo(t)
	rate, text := int16(7), ""

	_ = repo.AddRateAndDescription(context.Background(), 7, 1, &rate, &text)
	for i, statement := range pool.statements {
		if !strings.HasPrefix(statement, `INSERT INTO "film_entries"`) {
			continue
		}

		if !strings.Contains(statement, `DO UPDATE SET "description"=$10`) {
			t.Fatalf("want the text written on conflict, got %q", statement)
		}
		args := pool.args[i]
		if !isNull(args[5]) || !isNull(args[9]) {
			t.Errorf("want NULL text, got insert %v, update %v", args[5], args[9])
		}
		return
	}
	t.Fatalf("no insert: %q", pool.statements)
}

func TestWatchedFilmIDsUsesWatchedSetRule(t *testing.T) {
	t.Parallel()
	repo, pool := newRecordingRepo(t)

	_, _ = repo.WatchedFilmIDs(context.Background(), 1)
	if len(pool.statements) != 1 {
		t.Fatalf("want one query, got %q", pool.statements)
	}
	query := pool.statements[0]
	for _, part := range []string{"JOIN films ON films.id = film_entries.film_id", seriesWatchedSQL, "films.kind <> 'series' AND film_entries.watched_at IS NOT NULL"} {
		if !strings.Contains(query, part) {
			t.Errorf("query lacks %q:\n%s", part, query)
		}
	}
}

func TestRatedFilmsUsesDerivedSeriesRating(t *testing.T) {
	t.Parallel()
	repo, pool := newRecordingRepo(t)

	_, _ = repo.RatedFilms(context.Background(), 1, 7, 50, film.KindSeries)
	if len(pool.statements) == 0 {
		t.Fatal("no query built")
	}
	query := pool.statements[0]
	if !strings.Contains(query, entryRateSQL+" >= $") || !strings.Contains(query, "ORDER BY "+entryRateSQL+" DESC") {
		t.Errorf("want the threshold and order on entryRateSQL:\n%s", query)
	}
	if strings.Contains(query, "film_entries.rate >= $") {
		t.Errorf("the film-only rate filter is back:\n%s", query)
	}
}
