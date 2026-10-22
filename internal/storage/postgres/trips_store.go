package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"unicode/utf8"

	"lifelog-api/internal/storage/postgres/tripsmigration"
	"lifelog-api/internal/trips"
	"lifelog-api/internal/trips/rates"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

type TripsStore struct {
	db *gorm.DB

	q     tripsQuerier
	inTx  bool
	dbErr error
}

type tripsQuerier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func NewTripsStore(db *gorm.DB) *TripsStore {
	s := &TripsStore{db: db}
	sqlDB, err := db.DB()
	if err != nil {
		s.dbErr = fmt.Errorf("postgres: trips store has no pool: %w", err)
		return s
	}
	s.q = sqlDB
	return s
}

var (
	_ trips.Store         = (*TripsStore)(nil)
	_ rates.SnapshotStore = (*TripsStore)(nil)
)

func (s *TripsStore) InTx(ctx context.Context, fn func(tx trips.Store) error) (err error) {
	if s.dbErr != nil {
		return s.dbErr
	}
	if s.inTx {
		return fn(s)
	}
	sqlDB, ok := s.q.(*sql.DB)
	if !ok {
		return errors.New("postgres: trips store is not bound to a pool")
	}
	tx, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := fn(&TripsStore{db: s.db, q: tx, inTx: true}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *TripsStore) guarded(ctx context.Context, fn func() error) error {
	if s.dbErr != nil {
		return s.dbErr
	}
	if !s.inTx {
		return fn()
	}
	if _, err := s.q.ExecContext(ctx, "SAVEPOINT trips_write"); err != nil {
		return err
	}
	if err := fn(); err != nil {
		if _, rbErr := s.q.ExecContext(ctx, "ROLLBACK TO SAVEPOINT trips_write"); rbErr != nil {
			return errors.Join(err, rbErr)
		}
		return err
	}
	_, err := s.q.ExecContext(ctx, "RELEASE SAVEPOINT trips_write")
	return err
}

const (
	tripsFKTripCurrency    = "fk_trips_currencies_currency_code"
	tripsFKExpenseCurrency = "fk_expenses_currencies_currency_code"
)

func tripsMapErr(err error, currency string) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch {
	case pgErr.Code == "23505" && pgErr.ConstraintName == tripsmigration.ActiveTripIndex:
		return trips.ErrActiveTripExists
	case pgErr.Code == "23503" && currency != "" &&
		(pgErr.ConstraintName == tripsFKTripCurrency || pgErr.ConstraintName == tripsFKExpenseCurrency):
		return trips.UnknownCurrency(currency)
	case trips.IsIntegrityClass(pgErr.Code):
		return &trips.IntegrityError{
			SQLState:   pgErr.Code,
			Constraint: pgErr.ConstraintName,
			Message:    pgErr.Message,
		}
	}
	return err
}

type tripsLengthCheck struct {
	field string
	value *string
	max   int
}

func tripsCheckLengths(checks ...tripsLengthCheck) error {
	for _, c := range checks {
		if c.value != nil && utf8.RuneCountInString(*c.value) > c.max {
			return trips.FieldTooLong(c.field, c.max)
		}
	}
	return nil
}

func (s *TripsStore) execCount(ctx context.Context, query string, args ...any) (int, error) {
	q, err := s.querier()
	if err != nil {
		return 0, err
	}
	res, err := q.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, tripsMapErr(err, "")
	}
	n, err := res.RowsAffected()
	return int(n), err
}

func (s *TripsStore) querier() (tripsQuerier, error) {
	if s.dbErr != nil {
		return nil, s.dbErr
	}
	return s.q, nil
}
