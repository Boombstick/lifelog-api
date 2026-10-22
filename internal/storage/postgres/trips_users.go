package postgres

import (
	"context"
	"database/sql"
	"errors"

	"lifelog-api/internal/trips"

	"github.com/google/uuid"
)

const tripsUserCols = `id, telegram_id, username, created_at, auth_user_id`

func scanTripsUser(row interface{ Scan(...any) error }) (*trips.User, error) {
	var u trips.User
	if err := row.Scan(&u.ID, &u.TelegramID, &u.Username, &u.CreatedAt, &u.AuthUserID); err != nil {
		return nil, err
	}
	u.CreatedAt = u.CreatedAt.UTC()
	return &u, nil
}

func (s *TripsStore) userBy(ctx context.Context, where string, arg any) (*trips.User, error) {
	q, err := s.querier()
	if err != nil {
		return nil, err
	}
	u, err := scanTripsUser(q.QueryRowContext(ctx,
		`SELECT `+tripsUserCols+` FROM users WHERE `+where+` LIMIT 1`, arg))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return u, err
}

func (s *TripsStore) UserByAuthID(ctx context.Context, authUserID uuid.UUID) (*trips.User, error) {
	return s.userBy(ctx, "auth_user_id = $1", authUserID)
}

func (s *TripsStore) UserByTelegramID(ctx context.Context, telegramID int64) (*trips.User, error) {
	return s.userBy(ctx, "telegram_id = $1", telegramID)
}

func (s *TripsStore) InsertUser(ctx context.Context, user *trips.User) error {
	if err := tripsCheckLengths(tripsLengthCheck{"username", &user.Username, trips.MaxUsernameLen}); err != nil {
		return err
	}
	return s.guarded(ctx, func() error {
		_, err := s.q.ExecContext(ctx,
			`INSERT INTO users (id, telegram_id, username, created_at, auth_user_id)
			 VALUES ($1, $2, $3, $4, $5)`,
			user.ID, user.TelegramID, user.Username, user.CreatedAt, user.AuthUserID)
		return tripsMapErr(err, "")
	})
}

func (s *TripsStore) SetUserAuthID(ctx context.Context, userID, authUserID uuid.UUID) error {
	return s.guarded(ctx, func() error {
		_, err := s.q.ExecContext(ctx, `UPDATE users SET auth_user_id = $2 WHERE id = $1`, userID, authUserID)
		return tripsMapErr(err, "")
	})
}

func (s *TripsStore) SetUserTelegramID(ctx context.Context, userID uuid.UUID, telegramID *int64) error {
	return s.guarded(ctx, func() error {
		_, err := s.q.ExecContext(ctx, `UPDATE users SET telegram_id = $2 WHERE id = $1`, userID, telegramID)
		return tripsMapErr(err, "")
	})
}

func (s *TripsStore) DeleteUser(ctx context.Context, userID uuid.UUID) error {
	q, err := s.querier()
	if err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, userID)
	return tripsMapErr(err, "")
}
