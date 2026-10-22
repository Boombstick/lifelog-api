package token

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const keyBits = 2048

type Signer struct {
	key      *rsa.PrivateKey
	kid      string
	issuer   string
	audience string
	ttl      time.Duration
}

type Claims struct {
	Role string `json:"role"`
	Name string `json:"name"`

	Family string `json:"fam,omitempty"`
	jwt.RegisteredClaims
}

func LoadOrCreateSigner(path, issuer, audience string, ttl time.Duration) (*Signer, error) {
	key, err := loadKey(path)
	if err != nil {
		return nil, err
	}
	if key == nil {
		if key, err = createKey(path); err != nil {
			return nil, err
		}
	}
	return &Signer{key: key, kid: keyID(&key.PublicKey), issuer: issuer, audience: audience, ttl: ttl}, nil
}

func loadKey(path string) (*rsa.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("чтение ключа подписи %s: %w", path, err)
	}

	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, fmt.Errorf("файл ключа %s не содержит PEM-блока", path)
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("разбор ключа подписи %s: %w", path, err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("ключ %s не RSA, а %T", path, parsed)
	}
	return key, nil
}

func createKey(path string) (*rsa.PrivateKey, error) {
	key, err := rsa.GenerateKey(rand.Reader, keyBits)
	if err != nil {
		return nil, fmt.Errorf("генерация ключа подписи: %w", err)
	}

	encoded, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("сериализация ключа подписи: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("каталог для ключа подписи: %w", err)
	}
	body := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded})
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return nil, fmt.Errorf("запись ключа подписи %s: %w", path, err)
	}
	return key, nil
}

func keyID(pub *rsa.PublicKey) string {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return "0"
	}
	sum := sha256.Sum256(der)
	return base64.RawURLEncoding.EncodeToString(sum[:8])
}

func (s *Signer) Issue(userID uuid.UUID, role, name string, family uuid.UUID) (string, time.Time, error) {
	now := time.Now()
	expires := now.Add(s.ttl)

	claims := Claims{
		Role:   role,
		Name:   name,
		Family: familyClaim(family),
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID.String(),
			Issuer:    s.issuer,
			Audience:  jwt.ClaimStrings{s.audience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expires),
			ID:        uuid.NewString(),
		},
	}

	jwtToken := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	jwtToken.Header["kid"] = s.kid

	signed, err := jwtToken.SignedString(s.key)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("подпись токена: %w", err)
	}
	return signed, expires, nil
}

func (s *Signer) Parse(raw string) (*Claims, error) {
	parsed, err := jwt.ParseWithClaims(raw, &Claims{},
		func(*jwt.Token) (any, error) { return &s.key.PublicKey, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
		jwt.WithIssuer(s.issuer),
		jwt.WithAudience(s.audience),
		jwt.WithExpirationRequired())
	if err != nil {
		return nil, err
	}
	claims, ok := parsed.Claims.(*Claims)
	if !ok {
		return nil, jwt.ErrTokenInvalidClaims
	}
	return claims, nil
}

func (s *Signer) TTL() time.Duration { return s.ttl }

type JWK struct {
	Kty string `json:"kty"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type JWKS struct {
	Keys []JWK `json:"keys"`
}

func (s *Signer) JWKS() JWKS {
	pub := &s.key.PublicKey
	return JWKS{Keys: []JWK{{
		Kty: "RSA",
		Use: "sig",
		Alg: "RS256",
		Kid: s.kid,
		N:   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		E:   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
	}}}
}

func familyClaim(family uuid.UUID) string {
	if family == uuid.Nil {
		return ""
	}
	return family.String()
}

func (s *Signer) KeyID() string {
	if s == nil || s.key == nil {
		return ""
	}
	return s.kid
}
