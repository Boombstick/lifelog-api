package authsvc

import (
	"context"
	"time"

	"github.com/google/uuid"
)

func (s *Service) Subscribe(ctx context.Context, followerID uuid.UUID, login string) (*SubscriptionView, error) {
	target, err := s.repo.UserByLogin(ctx, normalizeLogin(login))
	if err != nil {
		return nil, err
	}
	if target.ID == followerID {
		return nil, ErrSelfSubscription
	}
	blockedByTarget, blockedByFollower, err := s.repo.BlockState(ctx, followerID, target.ID)
	if err != nil {
		return nil, err
	}

	if blockedByTarget {
		return nil, ErrUserNotFound
	}
	if blockedByFollower {
		return nil, ErrUserBlocked
	}

	status := SubscriptionActive
	if target.IsPrivate {
		status = SubscriptionPending
	}

	subscription := &Subscription{
		FollowerID: followerID,
		TargetID:   target.ID,
		Status:     status,
		CreatedAt:  time.Now(),
	}
	if err := s.repo.Subscribe(ctx, subscription); err != nil {
		return nil, err
	}
	s.invalidateFollows(ctx, followerID)

	return s.subscriptionTo(ctx, followerID, target)
}

func (s *Service) Unsubscribe(ctx context.Context, followerID, targetID uuid.UUID) error {
	if err := s.repo.Unsubscribe(ctx, followerID, targetID); err != nil {
		return err
	}
	s.invalidateFollows(ctx, followerID)
	return nil
}

func (s *Service) Subscriptions(ctx context.Context, followerID uuid.UUID) ([]SubscriptionView, error) {
	return s.repo.SubscriptionsOf(ctx, followerID)
}

func (s *Service) Subscribers(ctx context.Context, targetID uuid.UUID) ([]SubscriptionView, error) {
	return s.repo.SubscribersOf(ctx, targetID)
}

func (s *Service) ApproveSubscriber(ctx context.Context, targetID, followerID uuid.UUID) error {
	if err := s.repo.SetSubscriptionStatus(ctx, followerID, targetID, SubscriptionActive); err != nil {
		return err
	}
	s.invalidateFollows(ctx, followerID)
	return nil
}

func (s *Service) DeclineSubscriber(ctx context.Context, targetID, followerID uuid.UUID) error {
	return s.Unsubscribe(ctx, followerID, targetID)
}

func (s *Service) Following(ctx context.Context, followerID uuid.UUID) ([]uuid.UUID, error) {
	ids, err := s.repo.FollowingOf(ctx, followerID)
	if ids == nil {
		ids = []uuid.UUID{}
	}
	return ids, err
}

func (s *Service) FollowersAmong(ctx context.Context, targetID uuid.UUID, candidates []uuid.UUID) ([]uuid.UUID, error) {
	ids, err := s.repo.FollowersAmong(ctx, targetID, candidates)
	if ids == nil {
		ids = []uuid.UUID{}
	}
	return ids, err
}

const invalidateFollowsTimeout = 2 * time.Second

func (s *Service) invalidateFollows(ctx context.Context, userIDs ...uuid.UUID) {
	if s.notifier == nil {
		return
	}
	callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), invalidateFollowsTimeout)
	defer cancel()
	if err := s.notifier.InvalidateFollows(callCtx, userIDs...); err != nil {
		s.logger.Warn("follows invalidation not delivered", "users", userIDs, "error", err)
	}
}

func (s *Service) SetPrivate(ctx context.Context, userID uuid.UUID, isPrivate bool) error {
	return s.repo.SetPrivate(ctx, userID, isPrivate)
}

func (s *Service) subscriptionTo(ctx context.Context, followerID uuid.UUID, target *User) (*SubscriptionView, error) {
	subscriptions, err := s.repo.SubscriptionsOf(ctx, followerID)
	if err != nil {
		return nil, err
	}
	for _, view := range subscriptions {
		if view.UserID == target.ID {
			return &view, nil
		}
	}
	return nil, ErrSubscriptionAbsent
}
