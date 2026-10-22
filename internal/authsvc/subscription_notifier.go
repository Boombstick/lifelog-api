package authsvc

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
)

func (n *HTTPMergeNotifier) InvalidateFollows(ctx context.Context, userIDs ...uuid.UUID) error {
	if len(userIDs) == 0 {
		return nil
	}
	ids := make([]string, 0, len(userIDs))
	for _, id := range userIDs {
		ids = append(ids, id.String())
	}
	payload, err := json.Marshal(map[string]any{"user_ids": ids})
	if err != nil {
		return err
	}

	var firstErr error
	for _, target := range n.subscriptionTargets {
		if err := n.postInternal(ctx, target+"/internal/follows/invalidate", payload); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (n *HTTPMergeNotifier) NotifyBlock(ctx context.Context, a, b uuid.UUID) error {
	payload, err := json.Marshal(map[string]string{"a": a.String(), "b": b.String()})
	if err != nil {
		return err
	}
	var firstErr error
	for _, target := range n.subscriptionTargets {
		if err := n.postInternal(ctx, target+"/internal/blocks/applied", payload); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
