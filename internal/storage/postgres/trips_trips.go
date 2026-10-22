package postgres

import (
	"context"
	"database/sql"
	"errors"

	"lifelog-api/internal/trips"

	"github.com/google/uuid"
)

const tripsTripSelect = `SELECT t.id, t.user_id, t.name, t.budget::text, t.start_date, t.end_date, t.status,
       t.city, t.country_code, t.currency_code, t.timezone, t.source, t.created_at, t.updated_at,
       co.name_ru
FROM trips t
LEFT JOIN countries co ON co.code = t.country_code`

func tripsTripDest(t *trips.Trip) []any {
	return []any{&t.ID, &t.UserID, &t.Name, &t.Budget, &t.StartDate, &t.EndDate, &t.Status,
		&t.City, &t.CountryCode, &t.CurrencyCode, &t.Timezone, &t.Source, &t.CreatedAt, &t.UpdatedAt,
		&t.CountryName}
}

func tripsFixTimes(t *trips.Trip) {
	t.CreatedAt = t.CreatedAt.UTC()
	t.UpdatedAt = t.UpdatedAt.UTC()
}

func scanTripsTrip(row interface{ Scan(...any) error }) (trips.Trip, error) {
	var t trips.Trip
	if err := row.Scan(tripsTripDest(&t)...); err != nil {
		return t, err
	}
	tripsFixTimes(&t)
	return t, nil
}

func (s *TripsStore) ActiveTrips(ctx context.Context, userID uuid.UUID) ([]trips.Trip, error) {
	return tripsQueryAll(ctx, s, scanTripsTrip,
		tripsTripSelect+` WHERE t.user_id = $1 AND t.status = 2 ORDER BY t.id LIMIT 2`, userID)
}

func (s *TripsStore) OwnedTrip(ctx context.Context, userID, tripID uuid.UUID) (*trips.Trip, error) {
	q, err := s.querier()
	if err != nil {
		return nil, err
	}
	t, err := scanTripsTrip(q.QueryRowContext(ctx, tripsTripSelect+` WHERE t.id = $1 AND t.user_id = $2`, tripID, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *TripsStore) TripsOfUser(ctx context.Context, userID uuid.UUID) ([]trips.Trip, error) {
	return tripsQueryAll(ctx, s, scanTripsTrip, tripsTripSelect+` WHERE t.user_id = $1 ORDER BY t.id`, userID)
}

func (s *TripsStore) TripHistory(ctx context.Context, userID uuid.UUID) ([]trips.TripSummary, error) {
	query := `SELECT t.id, t.user_id, t.name, t.budget::text, t.start_date, t.end_date, t.status,
       t.city, t.country_code, t.currency_code, t.timezone, t.source, t.created_at, t.updated_at,
       co.name_ru,
       COALESCE((SELECT SUM(e.amount_usd) FROM expenses e
                 WHERE e.trip_id = t.id AND e.duplicate_of_id IS NULL), 0)::text,
       (SELECT COUNT(*) FROM expenses e WHERE e.trip_id = t.id AND e.duplicate_of_id IS NULL)
FROM trips t
LEFT JOIN countries co ON co.code = t.country_code
WHERE t.user_id = $1
ORDER BY (t.status = 2) DESC, COALESCE(t.start_date, DATE '0001-01-01') DESC, t.created_at DESC`
	return tripsQueryAll(ctx, s, func(row interface{ Scan(...any) error }) (trips.TripSummary, error) {
		var ts trips.TripSummary
		dest := append(tripsTripDest(&ts.Trip), &ts.TotalUSD, &ts.ExpenseCount)
		if err := row.Scan(dest...); err != nil {
			return ts, err
		}
		tripsFixTimes(&ts.Trip)
		return ts, nil
	}, query, userID)
}

func tripsTripLengths(t *trips.Trip) error {
	if t.CurrencyCode != nil && len([]rune(*t.CurrencyCode)) > 3 {
		return trips.UnknownCurrency(*t.CurrencyCode)
	}
	return tripsCheckLengths(
		tripsLengthCheck{"city", t.City, trips.MaxCityLen},
		tripsLengthCheck{"timezone", t.Timezone, trips.MaxTimezoneLen},
	)
}

func tripsCurrencyOf(t *trips.Trip) string {
	if t.CurrencyCode == nil {
		return ""
	}
	return *t.CurrencyCode
}

func (s *TripsStore) InsertTrip(ctx context.Context, trip *trips.Trip) error {
	if err := tripsTripLengths(trip); err != nil {
		return err
	}
	return s.guarded(ctx, func() error {
		_, err := s.q.ExecContext(ctx,
			`INSERT INTO trips (id, user_id, name, budget, start_date, end_date, status, updated_at, created_at,
			                    city, country_code, currency_code, timezone, source)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
			trip.ID, trip.UserID, trip.Name, trip.Budget, trip.StartDate, trip.EndDate, int(trip.Status),
			trip.UpdatedAt, trip.CreatedAt, trip.City, trip.CountryCode, trip.CurrencyCode, trip.Timezone,
			int(trip.Source))
		return tripsMapErr(err, tripsCurrencyOf(trip))
	})
}

func (s *TripsStore) UpdateTrip(ctx context.Context, trip *trips.Trip) error {
	if err := tripsTripLengths(trip); err != nil {
		return err
	}
	return s.guarded(ctx, func() error {
		_, err := s.q.ExecContext(ctx,
			`UPDATE trips SET user_id = $2, name = $3, budget = $4, start_date = $5, end_date = $6, status = $7,
			                  updated_at = $8, city = $9, country_code = $10, currency_code = $11, timezone = $12
			 WHERE id = $1`,
			trip.ID, trip.UserID, trip.Name, trip.Budget, trip.StartDate, trip.EndDate, int(trip.Status),
			trip.UpdatedAt, trip.City, trip.CountryCode, trip.CurrencyCode, trip.Timezone)
		return tripsMapErr(err, tripsCurrencyOf(trip))
	})
}

func (s *TripsStore) DeleteTrip(ctx context.Context, userID, tripID uuid.UUID) (bool, error) {
	q, err := s.querier()
	if err != nil {
		return false, err
	}
	result, err := q.ExecContext(ctx, `DELETE FROM trips WHERE id = $1 AND user_id = $2`, tripID, userID)
	if err != nil {
		return false, tripsMapErr(err, "")
	}
	affected, err := result.RowsAffected()
	return affected > 0, err
}

func (s *TripsStore) TripIDByClientID(ctx context.Context, userID, clientID uuid.UUID) (*uuid.UUID, error) {
	q, err := s.querier()
	if err != nil {
		return nil, err
	}
	var id uuid.UUID
	err = q.QueryRowContext(ctx,
		`SELECT trip_id FROM trip_client_ids WHERE user_id = $1 AND client_id = $2`, userID, clientID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func (s *TripsStore) InsertTripClientID(ctx context.Context, userID, clientID, tripID uuid.UUID) error {
	return s.guarded(ctx, func() error {
		_, err := s.q.ExecContext(ctx,
			`INSERT INTO trip_client_ids (user_id, client_id, trip_id) VALUES ($1, $2, $3)`, userID, clientID, tripID)
		return tripsMapErr(err, "")
	})
}

func (s *TripsStore) TripClientIDs(ctx context.Context, tripID uuid.UUID) ([]uuid.UUID, error) {
	return tripsQueryAll(ctx, s, func(row interface{ Scan(...any) error }) (uuid.UUID, error) {
		var id uuid.UUID
		err := row.Scan(&id)
		return id, err
	}, `SELECT client_id FROM trip_client_ids WHERE trip_id = $1 ORDER BY client_id`, tripID)
}
