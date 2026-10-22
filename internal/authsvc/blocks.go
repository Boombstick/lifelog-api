package authsvc

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrSelfBlock   = errors.New("нельзя заблокировать себя")
	ErrUserBlocked = errors.New("пользователь заблокирован")
)

type Block struct {
	BlockerID uuid.UUID `gorm:"type:uuid;primaryKey"`
	BlockedID uuid.UUID `gorm:"type:uuid;primaryKey;index"`
	CreatedAt time.Time `gorm:"not null"`
}

func (Block) TableName() string { return "auth_blocks" }

type BlockView struct {
	UserID      uuid.UUID `json:"user_id"`
	Login       string    `json:"login"`
	DisplayName string    `json:"display_name"`
	CreatedAt   time.Time `json:"created_at"`
}

func (s *Service) Block(ctx context.Context, blockerID uuid.UUID, login string) (*BlockView, error) {
	target, err := s.repo.UserByLogin(ctx, normalizeLogin(login))
	if err != nil {
		return nil, err
	}
	if target.ID == blockerID {
		return nil, ErrSelfBlock
	}

	if err := s.repo.Block(ctx, &Block{BlockerID: blockerID, BlockedID: target.ID, CreatedAt: time.Now()}); err != nil {
		return nil, err
	}

	s.invalidateFollows(ctx, blockerID, target.ID)
	s.applyBlock(ctx, blockerID, target.ID)

	blocks, err := s.repo.BlocksOf(ctx, blockerID)
	if err != nil {
		return nil, err
	}
	for _, view := range blocks {
		if view.UserID == target.ID {
			return &view, nil
		}
	}
	return nil, ErrUserNotFound
}

var (
	blockSignalTimeout = 2 * time.Second
	blockSignalRetries = []time.Duration{300 * time.Millisecond, 1500 * time.Millisecond}
)

func (s *Service) applyBlock(ctx context.Context, a, b uuid.UUID) {
	if s.notifier == nil {
		return
	}
	detached := context.WithoutCancel(ctx)
	attempt := func() error {
		callCtx, cancel := context.WithTimeout(detached, blockSignalTimeout)
		defer cancel()
		return s.notifier.NotifyBlock(callCtx, a, b)
	}
	err := attempt()
	if err == nil {
		return
	}
	s.logger.Warn("block signal not delivered, retrying", "a", a, "b", b, "attempt", 1, "error", err)
	go func() {
		for i, delay := range blockSignalRetries {
			time.Sleep(delay)
			if err = attempt(); err == nil {
				return
			}
			s.logger.Warn("block signal not delivered", "a", a, "b", b, "attempt", i+2, "error", err)
		}
		s.logger.Error("block signal lost, catalog will clean up on read", "a", a, "b", b, "error", err)
	}()
}

func (s *Service) BlockRelations(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	ids, err := s.repo.BlockRelations(ctx, userID)
	if ids == nil {
		ids = []uuid.UUID{}
	}
	return ids, err
}

func (s *Service) Unblock(ctx context.Context, blockerID, blockedID uuid.UUID) error {
	if err := s.repo.Unblock(ctx, blockerID, blockedID); err != nil {
		return err
	}

	s.invalidateFollows(ctx, blockerID, blockedID)
	return nil
}

func (s *Service) Blocks(ctx context.Context, blockerID uuid.UUID) ([]BlockView, error) {
	blocks, err := s.repo.BlocksOf(ctx, blockerID)
	if err != nil {
		return nil, err
	}
	if blocks == nil {
		blocks = []BlockView{}
	}
	return blocks, nil
}
