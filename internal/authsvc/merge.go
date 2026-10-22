package authsvc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"lifelog-api/internal/randtoken"

	"github.com/google/uuid"
)

func (s *Service) EnsureTelegramUser(ctx context.Context, telegramID int64, displayName string) (*User, error) {
	user, err := s.repo.UserByTelegram(ctx, telegramID)
	if err == nil {
		return user, nil
	}
	if !errors.Is(err, ErrUserNotFound) {
		return nil, err
	}

	displayName, err = NormalizeDisplayName(displayName)
	if err != nil {
		displayName = fmt.Sprintf("tg-%d", telegramID)
	}
	user = &User{ID: uuid.New(), DisplayName: displayName, Role: RoleUser, CreatedAt: time.Now()}
	if err := s.repo.CreateUser(ctx, user); err != nil {
		return nil, err
	}
	if err := s.repo.LinkTelegram(ctx, user.ID, telegramID); err != nil {
		return nil, err
	}
	return user, nil
}

func (s *Service) ResolveTelegram(ctx context.Context, telegramID int64) (*User, error) {
	return s.repo.UserByTelegram(ctx, telegramID)
}

type LinkInvitation struct {
	Code     string    `json:"code"`
	DeepLink string    `json:"deep_link"`
	Expires  time.Time `json:"expires_at"`
}

func (s *Service) StartTelegramLink(ctx context.Context, userID uuid.UUID) (*LinkInvitation, error) {
	linked, err := s.hasTelegram(ctx, userID)
	if err != nil {
		return nil, err
	}
	if linked {
		return nil, ErrTelegramAlreadyLinked
	}

	code := randtoken.New(16)

	nonce := &LinkNonce{Code: code, UserID: userID, CreatedAt: time.Now(), ExpiresAt: time.Now().Add(nonceTTL)}
	if err := s.repo.ReplaceNonce(ctx, nonce); err != nil {
		return nil, err
	}

	return &LinkInvitation{
		Code:     code,
		DeepLink: fmt.Sprintf("https://t.me/%s?start=%s", s.botName, code),
		Expires:  nonce.ExpiresAt,
	}, nil
}

func (s *Service) ConfirmTelegramLink(ctx context.Context, code string, telegramID int64) (*User, error) {
	nonce, err := s.repo.ConsumeNonce(ctx, code)
	if err != nil {
		return nil, err
	}

	appUser, err := s.repo.UserByID(ctx, nonce.UserID)
	if err != nil {
		return nil, err
	}

	owner, err := s.repo.UserByTelegram(ctx, telegramID)
	switch {
	case errors.Is(err, ErrUserNotFound):
		owner = nil
	case err != nil:
		return nil, err
	case owner.ID == appUser.ID:
		return appUser, nil
	case !owner.IsSilent():
		return nil, ErrTelegramTaken
	}

	linked, err := s.hasTelegram(ctx, appUser.ID)
	if err != nil {
		return nil, err
	}
	if linked {
		return nil, ErrTelegramAlreadyLinked
	}

	if owner == nil {
		if err := s.repo.LinkTelegram(ctx, appUser.ID, telegramID); err != nil {
			return nil, err
		}
		return appUser, nil
	}

	if err := s.absorbSilent(ctx, owner.ID, appUser.ID); err != nil {
		return nil, err
	}
	return s.repo.UserByID(ctx, appUser.ID)
}

func (s *Service) UnlinkTelegram(ctx context.Context, userID uuid.UUID) error {
	return s.repo.UnlinkTelegram(ctx, userID)
}

func (s *Service) HasTelegram(ctx context.Context, userID uuid.UUID) (bool, error) {
	return s.hasTelegram(ctx, userID)
}

func (s *Service) hasTelegram(ctx context.Context, userID uuid.UUID) (bool, error) {
	links, err := s.repo.TelegramIDs(ctx, []uuid.UUID{userID})
	if err != nil {
		return false, err
	}
	_, ok := links[userID]
	return ok, nil
}

func (s *Service) absorbSilent(ctx context.Context, silent, into uuid.UUID) error {
	merge, err := s.repo.AbsorbSilent(ctx, silent, into)
	if err != nil {
		return err
	}
	s.logger.Info("тихая учётка слита", "from", silent, "into", into)
	return s.finishMerge(ctx, *merge)
}

func (s *Service) finishMerge(ctx context.Context, merge Merge) error {
	if s.notifier != nil {
		if err := s.notifier.NotifyMerge(ctx, merge.FromUser, merge.IntoUser); err != nil {
			s.logger.Error("сервисы данных не приняли слияние", "merge_id", merge.ID, "error", err)
			return fmt.Errorf("перенос данных: %w", err)
		}
	}
	return s.repo.CompleteMerge(ctx, merge.ID)
}

func (s *Service) ResumeMerges(ctx context.Context) error {
	pending, err := s.repo.PendingMerges(ctx)
	if err != nil {
		return err
	}
	for _, merge := range pending {
		if err := s.finishMerge(ctx, merge); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) TelegramIDs(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID]int64, error) {
	return s.repo.TelegramIDs(ctx, userIDs)
}
