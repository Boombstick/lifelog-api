package auth_test

import (
	"context"
	"errors"
	"testing"

	"lifelog-api/internal/auth"
)

type foreignKey string

func TestPrincipalFromContext(t *testing.T) {
	t.Parallel()

	want := auth.Principal{UserID: 7, Role: auth.RoleAdmin}

	tests := []struct {
		name    string
		ctx     func() context.Context
		want    auth.Principal
		wantOk  bool
		wantErr error
	}{
		{
			name:   "актор положен в контекст",
			ctx:    func() context.Context { return auth.WithPrincipal(context.Background(), want) },
			want:   want,
			wantOk: true,
		},
		{
			name:    "пустой контекст",
			ctx:     context.Background,
			wantErr: auth.ErrNoPrincipal,
		},
		{
			name: "чужое значение под строковым ключом не подхватывается",
			ctx: func() context.Context {
				return context.WithValue(context.Background(), foreignKey("principal"), want)
			},
			wantErr: auth.ErrNoPrincipal,
		},
		{
			name: "вложенный вызов перекрывает прежнего актора",
			ctx: func() context.Context {
				ctx := auth.WithPrincipal(context.Background(), auth.Principal{UserID: 1, Role: auth.RoleUser})
				return auth.WithPrincipal(ctx, want)
			},
			want:   want,
			wantOk: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := tc.ctx()

			got, ok := auth.PrincipalFromContext(ctx)
			if ok != tc.wantOk {
				t.Fatalf("PrincipalFromContext вернул ok = %v, ожидалось %v", ok, tc.wantOk)
			}
			if got != tc.want {
				t.Errorf("Principal = %+v, ожидался %+v", got, tc.want)
			}

			required, err := auth.RequirePrincipal(ctx)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("RequirePrincipal вернул ошибку %v, ожидалась %v", err, tc.wantErr)
			}
			if required != tc.want {
				t.Errorf("RequirePrincipal вернул %+v, ожидался %+v", required, tc.want)
			}
		})
	}
}
