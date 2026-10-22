package calendar

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"lifelog-api/internal/randtoken"
	"lifelog-api/internal/textutil"

	"github.com/google/uuid"
)

const (
	importBack  = 30 * 24 * time.Hour
	importAhead = 180 * 24 * time.Hour

	stateTTL = 10 * time.Minute
)

type GoogleRepository interface {
	SaveGoogleState(ctx context.Context, state *GoogleState) error

	TakeGoogleState(ctx context.Context, hash string, now time.Time) (*GoogleState, error)
	ForgetGoogleStates(ctx context.Context, before time.Time) error

	GoogleLink(ctx context.Context, userID uuid.UUID) (*GoogleLink, error)
	SaveGoogleLink(ctx context.Context, link *GoogleLink) error
	ActiveGoogleLinks(ctx context.Context) ([]GoogleLink, error)
	MarkGoogleSync(ctx context.Context, userID uuid.UUID, syncedAt *time.Time, lastError string, revoked bool) error

	DeleteGoogleLink(ctx context.Context, userID uuid.UUID) error

	ReplaceImported(ctx context.Context, userID uuid.UUID, events []ImportedEvent) error
}

type GoogleSource interface {
	AuthURL(state string) string
	Exchange(ctx context.Context, code string) (*GoogleGrant, error)
	Events(ctx context.Context, refreshToken string, from, to time.Time) ([]ImportedEvent, error)
	Revoke(ctx context.Context, refreshToken string) error
}

type GoogleStatus struct {
	Enabled    bool       `json:"enabled"`
	Connected  bool       `json:"connected"`
	Email      string     `json:"email,omitempty"`
	Revoked    bool       `json:"revoked"`
	LastSyncAt *time.Time `json:"last_sync_at,omitempty"`
	LastError  string     `json:"last_error,omitempty"`
}

type GoogleImport struct {
	repo   GoogleRepository
	google GoogleSource
	sealer *Sealer
	logger *slog.Logger
	now    func() time.Time
}

func NewGoogleImport(repo GoogleRepository, google GoogleSource, sealer *Sealer, logger *slog.Logger) *GoogleImport {
	if google == nil || sealer == nil {
		return nil
	}
	return &GoogleImport{
		repo:   repo,
		google: google,
		sealer: sealer,
		logger: logger,
		now:    func() time.Time { return time.Now().UTC() },
	}
}

func (g *GoogleImport) Connect(ctx context.Context, userID uuid.UUID) (string, error) {
	if g == nil {
		return "", ErrGoogleDisabled
	}
	state := randtoken.New(32)

	now := g.now()
	if err := g.repo.ForgetGoogleStates(ctx, now); err != nil {
		g.logger.Warn("could not trim expired google states", "error", err)
	}
	if err := g.repo.SaveGoogleState(ctx, &GoogleState{
		Hash:      randtoken.Hash(state),
		UserID:    userID,
		ExpiresAt: now.Add(stateTTL),
	}); err != nil {
		return "", err
	}
	return g.google.AuthURL(state), nil
}

func (g *GoogleImport) Complete(ctx context.Context, state, code string) (imported int, syncErr, err error) {
	if g == nil {
		return 0, nil, ErrGoogleDisabled
	}
	if state == "" || code == "" {
		return 0, nil, ErrGoogleBadState
	}
	pending, err := g.repo.TakeGoogleState(ctx, randtoken.Hash(state), g.now())
	if err != nil {
		return 0, nil, err
	}

	grant, err := g.google.Exchange(ctx, code)
	if err != nil {
		return 0, nil, err
	}

	if !slices.Contains(grant.Scopes, GoogleEventsScope) {
		_ = g.google.Revoke(ctx, grant.RefreshToken)
		return 0, nil, ErrGoogleScopeMissing
	}

	sealed, err := g.sealer.Seal(grant.RefreshToken)
	if err != nil {
		return 0, nil, err
	}
	if err := g.repo.SaveGoogleLink(ctx, &GoogleLink{
		UserID:       pending.UserID,
		RefreshToken: sealed,
		Email:        grant.Email,
	}); err != nil {
		return 0, nil, err
	}

	imported, syncErr = g.Sync(ctx, pending.UserID)
	return imported, syncErr, nil
}

func (g *GoogleImport) Sync(ctx context.Context, userID uuid.UUID) (int, error) {
	if g == nil {
		return 0, ErrGoogleDisabled
	}
	link, err := g.repo.GoogleLink(ctx, userID)
	if err != nil {
		return 0, err
	}
	if link.Revoked {
		return 0, ErrGoogleRevoked
	}
	return g.sync(ctx, link)
}

func (g *GoogleImport) sync(ctx context.Context, link *GoogleLink) (int, error) {
	token, err := g.sealer.Open(link.RefreshToken)
	if err != nil {
		g.logger.Error("google token cannot be opened, was the key rotated?", "user", link.UserID, "error", err)
		return 0, g.revoke(ctx, link.UserID, ErrGoogleRevoked)
	}

	now := g.now()
	events, err := g.google.Events(ctx, token, now.Add(-importBack), now.Add(importAhead))
	if errors.Is(err, ErrGoogleRevoked) || errors.Is(err, ErrGoogleScopeMissing) {
		return 0, g.revoke(ctx, link.UserID, err)
	}
	if err != nil {
		if markErr := g.repo.MarkGoogleSync(ctx, link.UserID, link.LastSyncAt, textutil.Truncate(err.Error(), 500), false); markErr != nil {
			g.logger.Warn("could not record a failed google sync", "user", link.UserID, "error", markErr)
		}
		return 0, err
	}

	for i := range events {
		events[i].UserID = link.UserID
	}
	if err := g.repo.ReplaceImported(ctx, link.UserID, events); err != nil {
		return 0, fmt.Errorf("store imported events: %w", err)
	}
	if err := g.repo.MarkGoogleSync(ctx, link.UserID, &now, "", false); err != nil {
		return 0, err
	}
	return len(events), nil
}

func (g *GoogleImport) revoke(ctx context.Context, userID uuid.UUID, cause error) error {
	if err := g.repo.ReplaceImported(ctx, userID, nil); err != nil {
		return err
	}
	if err := g.repo.MarkGoogleSync(ctx, userID, nil, textutil.Truncate(cause.Error(), 500), true); err != nil {
		return err
	}
	return cause
}

func (g *GoogleImport) SyncAll(ctx context.Context) error {
	if g == nil {
		return nil
	}
	links, err := g.repo.ActiveGoogleLinks(ctx)
	if err != nil {
		return err
	}
	for i := range links {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		count, err := g.sync(ctx, &links[i])
		if err != nil {
			g.logger.Warn("google sync failed", "user", links[i].UserID, "error", err)
			continue
		}
		g.logger.Info("google sync done", "user", links[i].UserID, "events", count)
	}
	return nil
}

func (g *GoogleImport) Status(ctx context.Context, userID uuid.UUID) (GoogleStatus, error) {
	if g == nil {
		return GoogleStatus{}, nil
	}
	link, err := g.repo.GoogleLink(ctx, userID)
	if errors.Is(err, ErrGoogleNotLinked) {
		return GoogleStatus{Enabled: true}, nil
	}
	if err != nil {
		return GoogleStatus{}, err
	}
	return GoogleStatus{
		Enabled:    true,
		Connected:  true,
		Email:      link.Email,
		Revoked:    link.Revoked,
		LastSyncAt: link.LastSyncAt,
		LastError:  link.LastError,
	}, nil
}

func (g *GoogleImport) Disconnect(ctx context.Context, userID uuid.UUID) error {
	if g == nil {
		return ErrGoogleDisabled
	}
	link, err := g.repo.GoogleLink(ctx, userID)
	if err != nil {
		return err
	}
	if token, err := g.sealer.Open(link.RefreshToken); err == nil {
		if err := g.google.Revoke(ctx, token); err != nil {
			g.logger.Warn("google did not confirm the revocation", "user", userID, "error", err)
		}
	}
	return g.repo.DeleteGoogleLink(ctx, userID)
}
