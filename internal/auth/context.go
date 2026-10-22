package auth

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

type Principal struct {
	UserID uint
	Role   Role

	AuthUserID uuid.UUID
}

type principalKey struct{}

var ErrNoPrincipal = errors.New("no principal in context")

func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, principal)
}
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

func RequirePrincipal(ctx context.Context) (Principal, error) {
	p, ok := PrincipalFromContext(ctx)
	if !ok {
		return Principal{}, ErrNoPrincipal
	}
	return p, nil
}
