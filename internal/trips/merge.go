package trips

import (
	"context"

	"github.com/google/uuid"
)

type MergeService struct {
	store Store
	cache IdentityCache
	clock Clock
}

func NewMergeService(store Store, cache IdentityCache, clock Clock) *MergeService {
	return &MergeService{store: store, cache: cache, clock: clock}
}

func (s *MergeService) Merge(ctx context.Context, req MergeRequest) error {
	if req.From == req.Into {
		return nil
	}
	var touched []uuid.UUID
	err := s.store.InTx(ctx, func(tx Store) error {
		touched = touched[:0]
		source, err := tx.UserByAuthID(ctx, req.From)
		if err != nil || source == nil {
			return err
		}
		touched = append(touched, source.ID)

		target, err := tx.UserByAuthID(ctx, req.Into)
		if err != nil {
			return err
		}
		if target == nil {
			return tx.SetUserAuthID(ctx, source.ID, req.Into)
		}
		touched = append(touched, target.ID)

		if err := s.moveTrips(ctx, tx, source.ID, target.ID); err != nil {
			return err
		}
		if err := tx.ReassignSyncData(ctx, source.ID, target.ID); err != nil {
			return err
		}

		if source.TelegramID != nil && target.TelegramID == nil {
			if err := tx.SetUserTelegramID(ctx, source.ID, nil); err != nil {
				return err
			}
			telegramID := *source.TelegramID
			if err := tx.SetUserTelegramID(ctx, target.ID, &telegramID); err != nil {
				return err
			}
		}
		return tx.DeleteUser(ctx, source.ID)
	})
	if err != nil {
		return err
	}
	for _, id := range touched {
		s.cache.InvalidateUser(id)
	}
	return nil
}

func (s *MergeService) moveTrips(ctx context.Context, tx Store, sourceID, targetID uuid.UUID) error {
	trips, err := tx.TripsOfUser(ctx, sourceID)
	if err != nil {
		return err
	}
	targetActive, err := tx.ActiveTrips(ctx, targetID)
	if err != nil {
		return err
	}
	targetHasActive := len(targetActive) > 0

	now := s.clock.Now()
	today := DateOf(now)
	for i := range trips {
		trip := &trips[i]
		if trip.Status == StatusActive {
			if targetHasActive {
				if err := trip.End(nil, today); err != nil {
					return err
				}
			} else {
				targetHasActive = true
			}
		}
		trip.UserID = targetID
		trip.UpdatedAt = now
		if err := tx.UpdateTrip(ctx, trip); err != nil {
			return err
		}
	}
	return nil
}
