package token_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"lifelog-api/internal/token"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func jwksServer(t *testing.T, signer *token.Signer) (*httptest.Server, *atomic.Bool) {
	t.Helper()

	down := &atomic.Bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if down.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(signer.JWKS())
	}))
	t.Cleanup(server.Close)
	return server, down
}

func TestПроверкаТокенаПоJWKS(t *testing.T) {
	t.Parallel()

	signer := newSigner(t, time.Hour)
	server, _ := jwksServer(t, signer)

	userID := uuid.New()
	raw, _, err := signer.Issue(userID, "user", "Тест", uuid.New())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	verifier := token.NewVerifier(server.URL, "lifelog-auth", "lifelog", time.Hour)
	claims, err := verifier.Parse(context.Background(), raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if claims.Subject != userID.String() {
		t.Errorf("sub = %q, ожидался %q", claims.Subject, userID)
	}
}

func TestУпавшийAuthНеЛомаетПроверку(t *testing.T) {
	t.Parallel()

	signer := newSigner(t, time.Hour)
	server, down := jwksServer(t, signer)

	raw, _, err := signer.Issue(uuid.New(), "user", "Тест", uuid.New())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	verifier := token.NewVerifier(server.URL, "lifelog-auth", "lifelog", time.Nanosecond)
	if _, err := verifier.Parse(context.Background(), raw); err != nil {
		t.Fatalf("первая проверка: %v", err)
	}

	down.Store(true)

	if _, err := verifier.Parse(context.Background(), raw); err != nil {
		t.Errorf("токен перестал приниматься, пока auth недоступен: %v", err)
	}
}

func TestНеизвестныйКлючПриУпавшемAuthОтвергается(t *testing.T) {
	t.Parallel()

	known := newSigner(t, time.Hour)
	server, down := jwksServer(t, known)

	verifier := token.NewVerifier(server.URL, "lifelog-auth", "lifelog", time.Nanosecond)
	warmup, _, err := known.Issue(uuid.New(), "user", "Тест", uuid.New())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := verifier.Parse(context.Background(), warmup); err != nil {
		t.Fatalf("прогрев кеша: %v", err)
	}

	stranger := newSigner(t, time.Hour)
	foreign, _, err := stranger.Issue(uuid.New(), "user", "Тест", uuid.New())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	down.Store(true)
	if _, err := verifier.Parse(context.Background(), foreign); err == nil {
		t.Error("токен неизвестного ключа принят")
	}
}

func TestVerifierRejectsTokensOfTheRightKey(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "key.pem")
	signer, err := token.LoadOrCreateSigner(path, "lifelog-auth", "lifelog", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	server, _ := jwksServer(t, signer)
	verifier := token.NewVerifier(server.URL, "lifelog-auth", "lifelog", time.Hour)

	sameKey := func(issuer, audience string, ttl time.Duration) string {
		t.Helper()
		other, err := token.LoadOrCreateSigner(path, issuer, audience, ttl)
		if err != nil {
			t.Fatal(err)
		}
		raw, _, err := other.Issue(uuid.New(), "user", "Тест", uuid.New())
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	noExpiry := func() string {
		t.Helper()
		pemBytes, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		key, err := jwt.ParseRSAPrivateKeyFromPEM(pemBytes)
		if err != nil {
			t.Fatal(err)
		}
		unsigned := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.RegisteredClaims{
			Subject: uuid.NewString(), Issuer: "lifelog-auth", Audience: jwt.ClaimStrings{"lifelog"},
		})
		unsigned.Header["kid"] = signer.KeyID()
		raw, err := unsigned.SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}

	cases := map[string]string{
		"foreign issuer":           sameKey("evil-auth", "lifelog", time.Hour),
		"foreign audience":         sameKey("lifelog-auth", "other-service", time.Hour),
		"expired past the leeway":  sameKey("lifelog-auth", "lifelog", -5*time.Minute),
		"no expiry at all":         noExpiry(),
		"HS256 with the known kid": hmacToken(t, signer.KeyID()),
	}
	for name, raw := range cases {
		if _, err := verifier.Parse(context.Background(), raw); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}

	if _, err := verifier.Parse(context.Background(), sameKey("lifelog-auth", "lifelog", -5*time.Second)); err != nil {
		t.Errorf("token within the leeway refused: %v", err)
	}
}

func hmacToken(t *testing.T, kid string) string {
	t.Helper()
	unsigned := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject: uuid.NewString(), Issuer: "lifelog-auth", Audience: jwt.ClaimStrings{"lifelog"},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	})
	unsigned.Header["kid"] = kid
	raw, err := unsigned.SignedString([]byte("public-key-as-secret"))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestKeyDroppedFromJWKSIsNoLongerTrusted(t *testing.T) {
	t.Parallel()

	first := newSigner(t, time.Hour)
	second := newSigner(t, time.Hour)
	var current atomic.Pointer[token.Signer]
	current.Store(first)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(current.Load().JWKS())
	}))
	t.Cleanup(server.Close)

	verifier := token.NewVerifier(server.URL, "lifelog-auth", "lifelog", time.Nanosecond)
	old, _, err := first.Issue(uuid.New(), "user", "Тест", uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifier.Parse(context.Background(), old); err != nil {
		t.Fatalf("before rotation: %v", err)
	}

	current.Store(second)

	time.Sleep(40 * time.Millisecond)
	if _, err := verifier.Parse(context.Background(), old); err == nil {
		t.Fatal("token of a key no longer in JWKS accepted")
	}
}

func TestРотацияКлючаПодхватываетсяБезПерезапуска(t *testing.T) {
	t.Parallel()

	first := newSigner(t, time.Hour)
	server, _ := jwksServer(t, first)
	verifier := token.NewVerifier(server.URL, "lifelog-auth", "lifelog", time.Nanosecond)

	oldToken, _, err := first.Issue(uuid.New(), "user", "Тест", uuid.New())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := verifier.Parse(context.Background(), oldToken); err != nil {
		t.Fatalf("проверка до ротации: %v", err)
	}

	second, err := token.LoadOrCreateSigner(filepath.Join(t.TempDir(), "key.pem"), "lifelog-auth", "lifelog", time.Hour)
	if err != nil {
		t.Fatalf("LoadOrCreateSigner: %v", err)
	}
	rotated := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(second.JWKS())
	}))
	t.Cleanup(rotated.Close)

	verifier = token.NewVerifier(rotated.URL, "lifelog-auth", "lifelog", time.Nanosecond)
	newToken, _, err := second.Issue(uuid.New(), "user", "Тест", uuid.New())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := verifier.Parse(context.Background(), newToken); err != nil {
		t.Errorf("токен нового ключа не принят: %v", err)
	}
}
