package token

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/sync/singleflight"
)

var ErrUnknownKey = errors.New("ключ подписи неизвестен")

type Verifier struct {
	url      string
	issuer   string
	audience string
	ttl      time.Duration
	client   *http.Client

	unknownKidInterval time.Duration
	warmDelay          time.Duration
	warmMaxDelay       time.Duration

	fetches singleflight.Group

	mu        sync.RWMutex
	keys      map[string]*rsa.PublicKey
	refreshed time.Time
	attempted time.Time
}

func NewVerifier(jwksURL, issuer, audience string, ttl time.Duration) *Verifier {
	return &Verifier{
		url:      jwksURL,
		issuer:   issuer,
		audience: audience,
		ttl:      ttl,
		client:   &http.Client{Timeout: 5 * time.Second},
		keys:     map[string]*rsa.PublicKey{},

		unknownKidInterval: 30 * time.Second,
		warmDelay:          time.Second,
		warmMaxDelay:       30 * time.Second,
	}
}

func (v *Verifier) Parse(ctx context.Context, raw string) (*Claims, error) {
	claims := &Claims{}
	parsed, err := jwt.ParseWithClaims(raw, claims,
		func(t *jwt.Token) (any, error) { return v.keyFor(ctx, t) },
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.audience),
		jwt.WithExpirationRequired(),

		jwt.WithLeeway(30*time.Second))
	if err != nil {
		return nil, err
	}
	if !parsed.Valid {
		return nil, jwt.ErrTokenInvalidClaims
	}
	return claims, nil
}

func (v *Verifier) keyFor(ctx context.Context, t *jwt.Token) (any, error) {
	kid, _ := t.Header["kid"].(string)

	key, fresh := v.cached(kid)
	if key != nil && fresh {
		return key, nil
	}
	unknown := key == nil
	if unknown && v.attemptedRecently() {
		return nil, ErrUnknownKey
	}

	err := v.refreshShared(ctx, unknown)
	if key, _ := v.cached(kid); key != nil {
		return key, nil
	}
	if err != nil {
		return nil, fmt.Errorf("обновление JWKS: %w", err)
	}
	return nil, ErrUnknownKey
}

func (v *Verifier) cached(kid string) (*rsa.PublicKey, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.keys[kid], time.Since(v.refreshed) < v.ttl
}

func (v *Verifier) attemptedRecently() bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return !v.attempted.IsZero() && time.Since(v.attempted) < v.unknownKidInterval
}

func (v *Verifier) refreshShared(ctx context.Context, unknown bool) error {
	flight := "stale"
	if unknown {
		flight = "unknown"
	}
	result := v.fetches.DoChan(flight, func() (any, error) {
		if unknown && v.attemptedRecently() {
			return nil, nil
		}
		if _, fresh := v.cached(""); !unknown && fresh {
			return nil, nil
		}
		return nil, v.fetch(context.WithoutCancel(ctx))
	})
	select {
	case r := <-result:
		return r.Err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (v *Verifier) fetch(ctx context.Context) error {
	err := v.refresh(ctx)
	v.mu.Lock()
	v.attempted = time.Now()
	v.mu.Unlock()
	return err
}

func (v *Verifier) Warm(ctx context.Context, logger *slog.Logger) {
	delay := v.warmDelay
	failed := false
	for !v.loaded() {
		err := v.fetch(ctx)
		if err == nil {
			logger.Info("jwks loaded", "url", v.url, "after_failures", failed)
			return
		}
		if ctx.Err() != nil {
			return
		}
		if !failed {
			failed = true
			logger.Warn("jwks fetch failed, retrying in background", "url", v.url, "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay = min(delay*2, v.warmMaxDelay)
	}
}

func (v *Verifier) loaded() bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return !v.refreshed.IsZero()
}

func (v *Verifier) refresh(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, v.url, nil)
	if err != nil {
		return err
	}
	response, err := v.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%s ответил %d", v.url, response.StatusCode)
	}

	var jwks JWKS
	if err := json.NewDecoder(response.Body).Decode(&jwks); err != nil {
		return err
	}

	keys := make(map[string]*rsa.PublicKey, len(jwks.Keys))
	for _, key := range jwks.Keys {
		if key.Kty != "RSA" {
			continue
		}
		parsed, err := key.publicKey()
		if err != nil {
			return err
		}
		keys[key.Kid] = parsed
	}
	if len(keys) == 0 {
		return errors.New("в JWKS нет ни одного RSA-ключа")
	}

	v.mu.Lock()
	v.keys = keys
	v.refreshed = time.Now()
	v.mu.Unlock()
	return nil
}

func (k JWK) publicKey() (*rsa.PublicKey, error) {
	modulus, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, fmt.Errorf("модуль ключа %s: %w", k.Kid, err)
	}
	exponent, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, fmt.Errorf("экспонента ключа %s: %w", k.Kid, err)
	}
	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(modulus),
		E: int(new(big.Int).SetBytes(exponent).Int64()),
	}, nil
}
