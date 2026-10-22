package authsvc_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"lifelog-api/internal/authsvc"

	"github.com/google/uuid"
)

type sessionsRepo struct {
	authsvc.Repository
	sessions []authsvc.DeviceSession
	err      error
}

func (r sessionsRepo) ActiveSessions(context.Context, uuid.UUID) ([]authsvc.DeviceSession, error) {
	return r.sessions, r.err
}

func newService(repo authsvc.Repository) *authsvc.Service {
	return authsvc.NewService(repo, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), time.Hour, "bot")
}

func TestТекущееУстройствоОтмечаетсяПоСемье(t *testing.T) {
	current := uuid.New()
	other := uuid.New()

	service := newService(sessionsRepo{sessions: []authsvc.DeviceSession{
		{FamilyID: other},
		{FamilyID: current},
	}})

	sessions, err := service.Sessions(context.Background(), uuid.New(), current.String())
	if err != nil {
		t.Fatalf("получение сессий: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("сессий вернулось %d, ожидалось 2", len(sessions))
	}
	if sessions[0].Current {
		t.Error("чужое устройство отмечено текущим")
	}
	if !sessions[1].Current {
		t.Error("текущее устройство не отмечено")
	}
}

func TestБезКлеймаСемьиТекущимНеОтмеченоНичего(t *testing.T) {
	service := newService(sessionsRepo{sessions: []authsvc.DeviceSession{
		{FamilyID: uuid.New()},
		{FamilyID: uuid.New()},
	}})

	sessions, err := service.Sessions(context.Background(), uuid.New(), "")
	if err != nil {
		t.Fatalf("получение сессий: %v", err)
	}
	for _, session := range sessions {
		if session.Current {
			t.Fatalf("устройство %s отмечено текущим при пустом клейме", session.FamilyID)
		}
	}
}

func TestПустойСписокСессийНеОшибка(t *testing.T) {
	service := newService(sessionsRepo{})

	sessions, err := service.Sessions(context.Background(), uuid.New(), uuid.NewString())
	if err != nil {
		t.Fatalf("получение сессий: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("сессий вернулось %d, ожидалось 0", len(sessions))
	}
}
