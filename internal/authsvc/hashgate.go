package authsvc

import (
	"context"

	"github.com/alexedwards/argon2id"
)

var hashSlots = make(chan struct{}, 2)

func withHashSlot[T any](ctx context.Context, fn func() (T, error)) (T, error) {
	select {
	case hashSlots <- struct{}{}:
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	}
	defer func() { <-hashSlots }()
	return fn()
}

func createHash(ctx context.Context, password string) (string, error) {
	return withHashSlot(ctx, func() (string, error) {
		return argon2id.CreateHash(password, argon2id.DefaultParams)
	})
}

func compareHash(ctx context.Context, password, hash string) (bool, error) {
	return withHashSlot(ctx, func() (bool, error) {
		return argon2id.ComparePasswordAndHash(password, hash)
	})
}
