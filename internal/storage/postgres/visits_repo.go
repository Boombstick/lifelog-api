package postgres

import (
	"context"
	"crypto/rand"

	"lifelog-api/internal/visits"

	"gorm.io/gorm"
)

type VisitRepository struct {
	db *gorm.DB
}

func NewVisitRepository(db *gorm.DB) *VisitRepository { return &VisitRepository{db: db} }

func (r *VisitRepository) Salt(ctx context.Context, day string) ([]byte, error) {
	fresh := make([]byte, 32)
	if _, err := rand.Read(fresh); err != nil {
		return nil, err
	}

	var salt []byte
	err := r.db.WithContext(ctx).Raw(`
		WITH inserted AS (
			INSERT INTO visit_salts (day, salt) VALUES (?, ?) ON CONFLICT (day) DO NOTHING RETURNING salt
		)
		SELECT salt FROM inserted UNION ALL SELECT salt FROM visit_salts WHERE day = ? LIMIT 1`,
		day, fresh, day).Row().Scan(&salt)
	return salt, err
}

func (r *VisitRepository) Touch(ctx context.Context, v visits.VisitorDay) (int, error) {
	var total int
	err := r.db.WithContext(ctx).Raw(`
		INSERT INTO visit_days (day, visitor, first_seen, last_seen, requests, user_agent)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (day, visitor) DO UPDATE SET
			first_seen = LEAST(visit_days.first_seen, EXCLUDED.first_seen),
			last_seen = GREATEST(visit_days.last_seen, EXCLUDED.last_seen),
			requests = visit_days.requests + EXCLUDED.requests,
			user_agent = CASE WHEN EXCLUDED.user_agent <> '' AND EXCLUDED.last_seen >= visit_days.last_seen
				THEN EXCLUDED.user_agent ELSE visit_days.user_agent END
		RETURNING requests`,
		v.Day, v.Visitor, v.FirstSeen, v.LastSeen, v.Requests, v.UserAgent).Scan(&total).Error
	return total, err
}

func (r *VisitRepository) AddRequests(ctx context.Context, requests []visits.StoredRequest) error {
	if len(requests) == 0 {
		return nil
	}
	rows := make([]map[string]any, len(requests))
	for i, q := range requests {
		rows[i] = map[string]any{
			"day": q.Day, "visitor": q.Visitor, "at": q.At, "service": q.Service,
			"method": q.Method, "path": q.Path, "status": q.Status, "duration_ms": q.DurationMS,
		}
	}
	return r.db.WithContext(ctx).Table("visit_requests").CreateInBatches(rows, 500).Error
}

func (r *VisitRepository) TrimRequests(ctx context.Context, day, visitor string, keep int) error {
	return r.db.WithContext(ctx).Exec(`
		DELETE FROM visit_requests WHERE id IN (
			SELECT id FROM visit_requests WHERE day = ? AND visitor = ? ORDER BY at DESC, id DESC OFFSET ?
		)`, day, visitor, keep).Error
}

func (r *VisitRepository) Prune(ctx context.Context, saltsBefore, daysBefore string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, statement := range []struct {
			query string
			arg   string
		}{
			{`DELETE FROM visit_salts WHERE day < ?`, saltsBefore},
			{`DELETE FROM visit_requests WHERE day < ?`, daysBefore},
			{`DELETE FROM visit_days WHERE day < ?`, daysBefore},
		} {
			if err := tx.Exec(statement.query, statement.arg).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *VisitRepository) Days(ctx context.Context, from string) ([]visits.DayCount, error) {
	var days []visits.DayCount
	err := r.db.WithContext(ctx).Raw(`
		SELECT to_char(day, 'YYYY-MM-DD') AS day, count(*) AS visitors, sum(requests) AS requests
		FROM visit_days WHERE day >= ? GROUP BY day ORDER BY day`, from).Scan(&days).Error
	return days, err
}

func (r *VisitRepository) Visitors(ctx context.Context, day string) ([]visits.VisitorDay, error) {
	var visitors []visits.VisitorDay
	err := r.db.WithContext(ctx).Raw(`
		SELECT to_char(day, 'YYYY-MM-DD') AS day, visitor, first_seen, last_seen, requests, user_agent
		FROM visit_days WHERE day = ? ORDER BY last_seen DESC`, day).Scan(&visitors).Error
	return visitors, err
}

func (r *VisitRepository) Requests(ctx context.Context, day, visitor string) ([]visits.StoredRequest, error) {
	var requests []visits.StoredRequest
	err := r.db.WithContext(ctx).Raw(`
		SELECT to_char(day, 'YYYY-MM-DD') AS day, visitor, at, service, method, path, status, duration_ms
		FROM visit_requests WHERE day = ? AND visitor = ? ORDER BY at DESC, id DESC`, day, visitor).Scan(&requests).Error
	return requests, err
}
