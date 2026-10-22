package postgres

import (
	"context"
	"database/sql"
	"errors"

	"lifelog-api/internal/trips"

	"github.com/google/uuid"
)

const tripsExpenseSelect = `SELECT e.id, e.trip_id, e.amount::text, e.amount_usd::text, e.occurred_at, e.description,
       e.currency_code, e.exchange_rates_snapshot_id, e.category_id, e.created_at, e.client_id,
       e.duplicate_of_id, e.source,
       c.name, t.user_id, t.name, t.city
FROM expenses e
JOIN trips t ON t.id = e.trip_id
JOIN expense_categories c ON c.id = e.category_id`

func scanTripsExpenseDetail(row interface{ Scan(...any) error }) (trips.ExpenseDetail, error) {
	var d trips.ExpenseDetail
	err := row.Scan(&d.ID, &d.TripID, &d.Amount, &d.AmountUSD, &d.OccurredAt, &d.Description,
		&d.CurrencyCode, &d.SnapshotID, &d.CategoryID, &d.CreatedAt, &d.ClientID,
		&d.DuplicateOfID, &d.Source,
		&d.CategoryName, &d.TripUserID, &d.TripName, &d.TripCity)
	d.CreatedAt = d.CreatedAt.UTC()
	return d, err
}

func (s *TripsStore) expenseDetail(ctx context.Context, where string, args ...any) (*trips.ExpenseDetail, error) {
	q, err := s.querier()
	if err != nil {
		return nil, err
	}
	d, err := scanTripsExpenseDetail(q.QueryRowContext(ctx, tripsExpenseSelect+` WHERE `+where, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *TripsStore) ExpenseByClientID(ctx context.Context, clientID uuid.UUID) (*trips.ExpenseDetail, error) {
	return s.expenseDetail(ctx, `e.client_id = $1`, clientID)
}

func (s *TripsStore) OwnedExpense(ctx context.Context, userID uuid.UUID, id int64) (*trips.ExpenseDetail, error) {
	return s.expenseDetail(ctx, `e.id = $1 AND t.user_id = $2`, id, userID)
}

func (s *TripsStore) LookAlike(ctx context.Context, q trips.LookAlikeQuery) (*int64, error) {
	db, err := s.querier()
	if err != nil {
		return nil, err
	}
	var id int64
	err = db.QueryRowContext(ctx,
		`SELECT id FROM expenses
		 WHERE trip_id = $1 AND amount = $2::numeric AND currency_code = $3
		   AND occurred_at = $4 AND source <> $5 AND duplicate_of_id IS NULL
		 ORDER BY id LIMIT 1`,
		q.TripID, q.Amount, q.CurrencyCode, q.OccurredAt, int(q.Source)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func (s *TripsStore) InsertExpense(ctx context.Context, expense *trips.Expense) error {
	if err := tripsCheckLengths(tripsLengthCheck{"description", &expense.Description, trips.MaxDescriptionLen}); err != nil {
		return err
	}
	var tripID *uuid.UUID
	if expense.TripID != uuid.Nil {
		tripID = &expense.TripID
	}
	return s.guarded(ctx, func() error {
		err := s.q.QueryRowContext(ctx,
			`INSERT INTO expenses (amount, amount_usd, occurred_at, description, currency_code,
			                       exchange_rates_snapshot_id, category_id, created_at, trip_id,
			                       client_id, duplicate_of_id, source)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
			 RETURNING id`,
			expense.Amount, expense.AmountUSD, expense.OccurredAt, expense.Description, expense.CurrencyCode,
			expense.SnapshotID, expense.CategoryID, expense.CreatedAt, tripID,
			expense.ClientID, expense.DuplicateOfID, int(expense.Source)).Scan(&expense.ID)
		return tripsMapErr(err, expense.CurrencyCode)
	})
}

func (s *TripsStore) ListExpenses(ctx context.Context, filter trips.ExpenseFilter) ([]trips.ExpenseDetail, error) {
	return tripsQueryAll(ctx, s, scanTripsExpenseDetail,
		tripsExpenseSelect+`
		WHERE t.user_id = $1
		  AND (($2::uuid IS NULL AND t.status = 2) OR e.trip_id = $2::uuid)
		  AND ($3::bigint IS NULL OR e.category_id = $3::bigint)
		ORDER BY e.occurred_at DESC, e.id DESC`,
		filter.UserID, filter.TripID, filter.CategoryID)
}

const tripsRangeFilter = ` AND ($2::date IS NULL OR e.occurred_at >= $2) AND ($3::date IS NULL OR e.occurred_at <= $3)`

func (s *TripsStore) TotalAmount(ctx context.Context, userID uuid.UUID, r trips.DateRange) (trips.Decimal, error) {
	var total trips.Decimal
	q, err := s.querier()
	if err != nil {
		return total, err
	}
	err = q.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(e.amount), 0)::text
		 FROM expenses e JOIN trips t ON t.id = e.trip_id
		 WHERE t.user_id = $1 AND t.status = 2 AND e.duplicate_of_id IS NULL`+tripsRangeFilter,
		userID, r.From, r.To).Scan(&total)
	return total, err
}

func (s *TripsStore) ReportRows(ctx context.Context, userID uuid.UUID, r trips.DateRange) ([]trips.ReportRow, error) {
	return tripsQueryAll(ctx, s, func(row interface{ Scan(...any) error }) (trips.ReportRow, error) {
		var rr trips.ReportRow
		err := row.Scan(&rr.CategoryName, &rr.Amount, &rr.CurrencyCode)
		return rr, err
	}, `SELECT c.name, e.amount::text, e.currency_code
		FROM expenses e
		JOIN trips t ON t.id = e.trip_id
		JOIN expense_categories c ON c.id = e.category_id
		WHERE t.user_id = $1 AND e.duplicate_of_id IS NULL`+tripsRangeFilter+`
		ORDER BY e.currency_code`, userID, r.From, r.To)
}

func (s *TripsStore) BuilderRows(ctx context.Context, userID uuid.UUID, r trips.DateRange) ([]trips.ExpenseDetail, error) {
	return tripsQueryAll(ctx, s, scanTripsExpenseDetail,
		tripsExpenseSelect+` WHERE t.user_id = $1 AND e.duplicate_of_id IS NULL`+tripsRangeFilter, userID, r.From, r.To)
}

func (s *TripsStore) FlaggedExpenses(ctx context.Context, userID uuid.UUID) ([]trips.ExpenseDetail, error) {
	return tripsQueryAll(ctx, s, scanTripsExpenseDetail,
		tripsExpenseSelect+` WHERE t.user_id = $1 AND e.duplicate_of_id IS NOT NULL ORDER BY e.created_at DESC, e.id DESC`, userID)
}

func (s *TripsStore) ClearDuplicateFlag(ctx context.Context, userID uuid.UUID, id int64) (bool, error) {
	return s.execOwned(ctx,
		`UPDATE expenses e SET duplicate_of_id = NULL
		 FROM trips t WHERE t.id = e.trip_id AND t.user_id = $1 AND e.id = $2`, userID, id)
}

func (s *TripsStore) DeleteExpense(ctx context.Context, userID uuid.UUID, id int64) (bool, error) {
	return s.execOwned(ctx,
		`DELETE FROM expenses e USING trips t
		 WHERE t.id = e.trip_id AND t.user_id = $1 AND e.id = $2`, userID, id)
}

func (s *TripsStore) DeleteExpensesOfTrip(ctx context.Context, tripID uuid.UUID) (int, error) {
	return s.execCount(ctx, `DELETE FROM expenses WHERE trip_id = $1`, tripID)
}

func (s *TripsStore) execOwned(ctx context.Context, query string, args ...any) (bool, error) {
	q, err := s.querier()
	if err != nil {
		return false, err
	}
	res, err := q.ExecContext(ctx, query, args...)
	if err != nil {
		return false, tripsMapErr(err, "")
	}
	n, err := res.RowsAffected()
	return n > 0, err
}
