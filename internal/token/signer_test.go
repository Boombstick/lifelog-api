package token_test

import (
	"encoding/base64"
	"path/filepath"
	"testing"
	"time"

	"lifelog-api/internal/token"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func newSigner(t *testing.T, ttl time.Duration) *token.Signer {
	t.Helper()

	signer, err := token.LoadOrCreateSigner(filepath.Join(t.TempDir(), "key.pem"), "lifelog-auth", "lifelog", ttl)
	if err != nil {
		t.Fatalf("LoadOrCreateSigner: %v", err)
	}
	return signer
}

func TestТокенПроходитТудаИОбратно(t *testing.T) {
	t.Parallel()

	signer := newSigner(t, time.Hour)
	userID := uuid.New()

	raw, expires, err := signer.Issue(userID, "user", "Тест", uuid.New())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if !expires.After(time.Now()) {
		t.Errorf("срок жизни токена в прошлом: %v", expires)
	}

	claims, err := signer.Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if claims.Subject != userID.String() {
		t.Errorf("sub = %q, ожидался %q", claims.Subject, userID)
	}
	if claims.Role != "user" {
		t.Errorf("role = %q", claims.Role)
	}
	if claims.ID == "" {
		t.Error("у токена нет jti")
	}
}

func TestКлючПереживаетПерезапуск(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "key.pem")
	first, err := token.LoadOrCreateSigner(path, "lifelog-auth", "lifelog", time.Hour)
	if err != nil {
		t.Fatalf("первый запуск: %v", err)
	}

	raw, _, err := first.Issue(uuid.New(), "user", "Тест", uuid.New())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	second, err := token.LoadOrCreateSigner(path, "lifelog-auth", "lifelog", time.Hour)
	if err != nil {
		t.Fatalf("второй запуск: %v", err)
	}
	if _, err := second.Parse(raw); err != nil {
		t.Errorf("токен, выданный до перезапуска, перестал приниматься: %v", err)
	}

	if first.JWKS().Keys[0].Kid != second.JWKS().Keys[0].Kid {
		t.Error("kid сменился при перезапуске, кеш JWKS у сервисов протухнет зря")
	}
}

func TestЧужиеТокеныОтвергаются(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		token func(t *testing.T) string
	}{
		{
			name: "протухший",
			token: func(t *testing.T) string {
				raw, _, err := newSigner(t, -time.Minute).Issue(uuid.New(), "user", "Тест", uuid.New())
				if err != nil {
					t.Fatalf("Issue: %v", err)
				}
				return raw
			},
		},
		{
			name: "подписан другим ключом",
			token: func(t *testing.T) string {
				raw, _, err := newSigner(t, time.Hour).Issue(uuid.New(), "user", "Тест", uuid.New())
				if err != nil {
					t.Fatalf("Issue: %v", err)
				}
				return raw
			},
		},
		{
			name: "алгоритм подменён на HS256",
			token: func(t *testing.T) string {
				claims := jwt.RegisteredClaims{
					Subject:   uuid.NewString(),
					Issuer:    "lifelog-auth",
					Audience:  jwt.ClaimStrings{"lifelog"},
					ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
				}
				raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("public-key-as-secret"))
				if err != nil {
					t.Fatalf("подготовка токена: %v", err)
				}
				return raw
			},
		},
		{
			name: "чужая аудитория",
			token: func(t *testing.T) string {
				other, err := token.LoadOrCreateSigner(filepath.Join(t.TempDir(), "key.pem"), "lifelog-auth", "другой-сервис", time.Hour)
				if err != nil {
					t.Fatalf("LoadOrCreateSigner: %v", err)
				}
				raw, _, err := other.Issue(uuid.New(), "user", "Тест", uuid.New())
				if err != nil {
					t.Fatalf("Issue: %v", err)
				}
				return raw
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			signer := newSigner(t, time.Hour)
			if _, err := signer.Parse(testCase.token(t)); err == nil {
				t.Error("токен принят, хотя не должен был")
			}
		})
	}
}

func TestJWKSПригоденДляВнешнейПроверки(t *testing.T) {
	t.Parallel()

	jwks := newSigner(t, time.Hour).JWKS()
	if len(jwks.Keys) != 1 {
		t.Fatalf("ключей в JWKS: %d", len(jwks.Keys))
	}

	key := jwks.Keys[0]
	if key.Kty != "RSA" || key.Alg != "RS256" || key.Use != "sig" {
		t.Errorf("неожиданные параметры ключа: %+v", key)
	}
	for name, value := range map[string]string{"n": key.N, "e": key.E, "kid": key.Kid} {
		if value == "" {
			t.Errorf("поле %s пустое", name)
		}
	}

	if _, err := base64.RawURLEncoding.DecodeString(key.N); err != nil {
		t.Errorf("модуль ключа не в base64url без выравнивания: %v", err)
	}
}
