package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"lifelog-api/internal/trips"

	"github.com/google/uuid"
)

const tripsPhotoSelect = `SELECT p.id, p.trip_id, p.stored_file_name, p.original_file_name, p.content_type,
       p.size_bytes, p.caption, p.created_at
FROM trip_photos p
JOIN trips t ON t.id = p.trip_id`

func scanTripsPhoto(row interface{ Scan(...any) error }) (trips.Photo, error) {
	var p trips.Photo
	err := row.Scan(&p.ID, &p.TripID, &p.StoredFileName, &p.OriginalFileName, &p.ContentType,
		&p.SizeBytes, &p.Caption, &p.CreatedAt)
	p.CreatedAt = p.CreatedAt.UTC()
	return p, err
}

func (s *TripsStore) TripPhotos(ctx context.Context, userID, tripID uuid.UUID) ([]trips.Photo, error) {
	return tripsQueryAll(ctx, s, scanTripsPhoto,
		tripsPhotoSelect+` WHERE p.trip_id = $1 AND t.user_id = $2 ORDER BY p.created_at DESC`, tripID, userID)
}

func (s *TripsStore) OwnedPhoto(ctx context.Context, userID, photoID uuid.UUID) (*trips.Photo, error) {
	q, err := s.querier()
	if err != nil {
		return nil, err
	}
	p, err := scanTripsPhoto(q.QueryRowContext(ctx, tripsPhotoSelect+` WHERE p.id = $1 AND t.user_id = $2`, photoID, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

const storedFileNameMax = 128

func (s *TripsStore) InsertPhoto(ctx context.Context, photo *trips.Photo) error {
	if err := tripsCheckLengths(
		tripsLengthCheck{"file_name", photo.OriginalFileName, trips.MaxOriginalFileNameLen},
		tripsLengthCheck{"caption", photo.Caption, trips.MaxCaptionLen},
		tripsLengthCheck{"content_type", &photo.ContentType, trips.MaxContentTypeLen},
		tripsLengthCheck{"stored_file_name", &photo.StoredFileName, storedFileNameMax},
	); err != nil {
		return err
	}
	return s.guarded(ctx, func() error {
		_, err := s.q.ExecContext(ctx,
			`INSERT INTO trip_photos (id, trip_id, stored_file_name, original_file_name, content_type,
			                         size_bytes, caption, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			photo.ID, photo.TripID, photo.StoredFileName, photo.OriginalFileName, photo.ContentType,
			photo.SizeBytes, photo.Caption, photo.CreatedAt)
		return tripsMapErr(err, "")
	})
}

func (s *TripsStore) DeletePhoto(ctx context.Context, photoID uuid.UUID) error {
	q, err := s.querier()
	if err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, `DELETE FROM trip_photos WHERE id = $1`, photoID)
	return tripsMapErr(err, "")
}

func (s *TripsStore) DeleteTripPhotos(ctx context.Context, tripID uuid.UUID) (int, error) {
	return s.execCount(ctx, `DELETE FROM trip_photos WHERE trip_id = $1`, tripID)
}

func (s *TripsStore) SyncResult(ctx context.Context, userID, opID uuid.UUID) (json.RawMessage, error) {
	q, err := s.querier()
	if err != nil {
		return nil, err
	}
	var text string
	err = q.QueryRowContext(ctx,
		`SELECT result_json::text FROM sync_operations WHERE user_id = $1 AND op_id = $2`, userID, opID).Scan(&text)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return json.RawMessage(text), nil
}

func (s *TripsStore) SaveSyncResult(ctx context.Context, userID, opID uuid.UUID, result json.RawMessage, processedAt time.Time) error {
	return s.guarded(ctx, func() error {
		_, err := s.q.ExecContext(ctx,
			`INSERT INTO sync_operations (user_id, op_id, result_json, processed_at) VALUES ($1, $2, $3::jsonb, $4)`,
			userID, opID, string(result), processedAt)
		return tripsMapErr(err, "")
	})
}

func (s *TripsStore) ReassignSyncData(ctx context.Context, fromUserID, toUserID uuid.UUID) error {
	return s.guarded(ctx, func() error {
		if _, err := s.q.ExecContext(ctx,
			`UPDATE trip_client_ids SET user_id = $2 WHERE user_id = $1`, fromUserID, toUserID); err != nil {
			return tripsMapErr(err, "")
		}
		_, err := s.q.ExecContext(ctx,
			`UPDATE sync_operations SET user_id = $2 WHERE user_id = $1`, fromUserID, toUserID)
		return tripsMapErr(err, "")
	})
}

const tripsSnapshotSelect = `SELECT id, base_currency, rates::text, created_at FROM exchange_rate_snapshots`

func (s *TripsStore) snapshot(ctx context.Context, tail string, args ...any) (*trips.Snapshot, error) {
	q, err := s.querier()
	if err != nil {
		return nil, err
	}
	var (
		snap  trips.Snapshot
		rates string
	)
	err = q.QueryRowContext(ctx, tripsSnapshotSelect+tail, args...).Scan(&snap.ID, &snap.BaseCurrency, &rates, &snap.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal([]byte(rates), &snap.Rates); err != nil {
		return nil, err
	}
	snap.CreatedAt = snap.CreatedAt.UTC()
	return &snap, nil
}

func (s *TripsStore) LatestSnapshot(ctx context.Context) (*trips.Snapshot, error) {
	return s.snapshot(ctx, ` ORDER BY id DESC LIMIT 1`)
}

func (s *TripsStore) SnapshotsAround(ctx context.Context, at time.Time) (*trips.Snapshot, *trips.Snapshot, error) {
	before, err := s.snapshot(ctx, ` WHERE created_at <= $1 ORDER BY created_at DESC, id DESC LIMIT 1`, at)
	if err != nil {
		return nil, nil, err
	}
	after, err := s.snapshot(ctx, ` WHERE created_at > $1 ORDER BY created_at, id LIMIT 1`, at)
	if err != nil {
		return nil, nil, err
	}
	return before, after, nil
}

func (s *TripsStore) InsertSnapshot(ctx context.Context, snapshot *trips.Snapshot) error {
	values := snapshot.Rates
	if values == nil {
		values = map[string]trips.Decimal{}
	}
	rates, err := json.Marshal(values)
	if err != nil {
		return err
	}
	return s.guarded(ctx, func() error {
		err := s.q.QueryRowContext(ctx,
			`INSERT INTO exchange_rate_snapshots (base_currency, rates, created_at) VALUES ($1, $2::jsonb, $3) RETURNING id`,
			snapshot.BaseCurrency, string(rates), snapshot.CreatedAt).Scan(&snapshot.ID)
		return tripsMapErr(err, "")
	})
}
