package authsvc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	codeTTL         = 15 * time.Minute
	codeCooldown    = 60 * time.Second
	maxCodeAttempts = 5
	maxEmailLength  = 254

	codeAttemptWindow     = 24 * time.Hour
	maxCodeAttemptsPerDay = 20

	attachWindow     = time.Hour
	maxAttachPerHour = 5

	loginWindow      = 15 * time.Minute
	maxLoginFailures = 10

	PurposeRegister = "register"
	PurposeReset    = "reset"
	PurposeAttach   = "attach"
)

var (
	ErrInvalidEmail  = errors.New("некорректный email")
	ErrLoginRequired = errors.New("нужен логин")

	ErrCodeInvalid = errors.New("неверный или просроченный код")
	ErrCodeTooSoon = errors.New("код уже отправлен, повторить можно через минуту")
	ErrEmailTaken  = errors.New("email уже используется")
	ErrCodeLocked  = errors.New("слишком много попыток ввода кода, попробуйте через сутки")
	ErrAttachLimit = errors.New("слишком много попыток сменить почту, попробуйте через час")
)

const (
	codeCounterPrefix   = "code:"
	attachCounterPrefix = "attach-start:"
	loginCounterPrefix  = "login:"
)

type EmailCode struct {
	Key          string     `gorm:"size:300;primaryKey"`
	Purpose      string     `gorm:"size:16;not null"`
	Email        string     `gorm:"size:254;not null"`
	UserID       *uuid.UUID `gorm:"type:uuid"`
	Login        string     `gorm:"size:64"`
	DisplayName  string     `gorm:"size:128"`
	PasswordHash string     `gorm:"size:256"`
	CodeHash     string     `gorm:"size:64;not null"`
	Attempts     int        `gorm:"not null;default:0"`
	ExpiresAt    time.Time  `gorm:"index;not null"`
	CreatedAt    time.Time  `gorm:"not null"`
}

func (EmailCode) TableName() string { return "auth_email_codes" }

type Mail struct {
	To      string
	Subject string
	Body    string
	Code    string
}

type Mailer interface {
	Send(ctx context.Context, mail Mail) error
}

func (s *Service) SetMailer(mailer Mailer) { s.mailer = mailer }

func NormalizeEmail(raw string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(raw))
	if email == "" || len(email) > maxEmailLength {
		return "", ErrInvalidEmail
	}
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email {
		return "", ErrInvalidEmail
	}
	at := strings.LastIndex(email, "@")
	if at <= 0 || !strings.Contains(email[at+1:], ".") {
		return "", ErrInvalidEmail
	}
	return email, nil
}

func newCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", fmt.Errorf("verification code: %w", err)
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

func hashCode(key, code string) string {
	sum := sha256.Sum256([]byte(key + ":" + strings.TrimSpace(code)))
	return hex.EncodeToString(sum[:])
}

func (s *Service) cooldownActive(ctx context.Context, key string, now time.Time) (bool, error) {
	existing, err := s.repo.EmailCode(ctx, key)
	if errors.Is(err, ErrCodeInvalid) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return now.Sub(existing.CreatedAt) < codeCooldown, nil
}

func (s *Service) issueCode(ctx context.Context, record *EmailCode, subject, body string, withCode bool) error {
	code := ""
	if withCode {
		generated, err := newCode()
		if err != nil {
			return err
		}
		code = generated
		record.CodeHash = hashCode(record.Key, code)
		body = strings.ReplaceAll(body, "{code}", code)
	}
	if err := s.repo.SaveEmailCode(ctx, record); err != nil {
		return err
	}
	if err := s.mailer.Send(ctx, Mail{To: record.Email, Subject: subject, Body: body, Code: code}); err != nil {
		_ = s.repo.DeleteEmailCode(ctx, record.Key)
		return fmt.Errorf("send mail: %w", err)
	}
	return nil
}

func (s *Service) checkCode(ctx context.Context, key, code string, now time.Time) (*EmailCode, error) {
	tries, err := s.repo.HitRateCounter(ctx, codeCounterPrefix+key, now, codeAttemptWindow)
	if err != nil {
		return nil, err
	}
	if tries > maxCodeAttemptsPerDay {
		return nil, ErrCodeLocked
	}

	record, err := s.repo.TakeCodeAttempt(ctx, key, maxCodeAttempts, now)
	if err != nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare([]byte(hashCode(key, code)), []byte(record.CodeHash)) != 1 {
		return nil, ErrCodeInvalid
	}
	return record, nil
}

func (s *Service) RegisterStart(ctx context.Context, rawEmail, login, password, displayName string) (time.Time, error) {
	email, err := NormalizeEmail(rawEmail)
	if err != nil {
		return time.Time{}, err
	}
	if login, err = NormalizeLogin(login); err != nil {
		return time.Time{}, err
	}
	if len([]rune(password)) < minPasswordLength {
		return time.Time{}, ErrWeakPassword
	}

	if strings.TrimSpace(displayName) == "" {
		displayName = login
	} else if displayName, err = NormalizeDisplayName(displayName); err != nil {
		return time.Time{}, err
	}

	if _, err := s.repo.UserByLogin(ctx, login); err == nil {
		return time.Time{}, ErrLoginTaken
	} else if !errors.Is(err, ErrUserNotFound) {
		return time.Time{}, err
	}

	now := time.Now()
	key := PurposeRegister + ":" + email
	if soon, err := s.cooldownActive(ctx, key, now); err != nil {
		return time.Time{}, err
	} else if soon {
		return time.Time{}, ErrCodeTooSoon
	}

	record := &EmailCode{Key: key, Purpose: PurposeRegister, Email: email, ExpiresAt: now.Add(codeTTL), CreatedAt: now}

	if _, err := s.repo.UserByEmail(ctx, email); err == nil {
		return record.ExpiresAt, s.issueCode(ctx, record,
			"Попытка регистрации в LifeLog",
			"На этот адрес уже зарегистрирован аккаунт LifeLog.\n\nЕсли это были вы — просто войдите или восстановите пароль. Если нет — ничего делать не нужно.",
			false)
	} else if !errors.Is(err, ErrUserNotFound) {
		return time.Time{}, err
	}

	hash, err := createHash(ctx, password)
	if err != nil {
		return time.Time{}, fmt.Errorf("hash password: %w", err)
	}
	record.Login = login
	record.DisplayName = displayName
	record.PasswordHash = hash

	return record.ExpiresAt, s.issueCode(ctx, record,
		"Код подтверждения LifeLog",
		"Ваш код подтверждения: {code}\n\nОн действует 15 минут. Если вы не регистрировались в LifeLog, просто проигнорируйте это письмо.",
		true)
}

func (s *Service) RegisterConfirm(ctx context.Context, rawEmail, code string, client Client) (*TokenPair, error) {
	email, err := NormalizeEmail(rawEmail)
	if err != nil {
		return nil, ErrCodeInvalid
	}
	key := PurposeRegister + ":" + email
	now := time.Now()

	record, err := s.checkCode(ctx, key, code, now)
	if err != nil {
		return nil, err
	}

	login := record.Login
	user := &User{
		ID:          uuid.New(),
		Login:       &login,
		Email:       &email,
		DisplayName: record.DisplayName,
		Role:        RoleUser,
		CreatedAt:   now,
	}
	if err := s.repo.CreateUserFromCode(ctx, key, record.CodeHash, user, record.PasswordHash); err != nil {
		return nil, err
	}
	return s.issuePair(ctx, user, uuid.New(), client)
}

func (s *Service) LoginByEmail(ctx context.Context, rawEmail, password string, client Client) (*TokenPair, error) {
	email, err := NormalizeEmail(rawEmail)
	if err != nil {
		return s.limitedSignIn(ctx, normalizeLogin(rawEmail), func() (*TokenPair, error) {
			_, _ = compareHash(ctx, password, decoyHash)
			return nil, ErrInvalidCredentials
		})
	}
	return s.limitedSignIn(ctx, email, func() (*TokenPair, error) {
		user, err := s.repo.UserByEmail(ctx, email)
		return s.passwordSignIn(ctx, user, err, password, client)
	})
}

func (s *Service) limitedSignIn(ctx context.Context, identifier string, signIn func() (*TokenPair, error)) (*TokenPair, error) {
	key := loginCounterPrefix + identifier
	hits, err := s.repo.HitRateCounter(ctx, key, time.Now(), loginWindow)
	if err != nil {
		return nil, err
	}
	if hits > maxLoginFailures {
		return nil, ErrLoginLocked
	}
	pair, err := signIn()
	if err != nil {
		return nil, err
	}
	if err := s.repo.ResetRateCounter(ctx, key); err != nil {
		s.logger.Warn("reset login counter failed", "error", err)
	}
	return pair, nil
}

func (s *Service) SignIn(ctx context.Context, identifier, password string, client Client) (*TokenPair, error) {
	if strings.Contains(identifier, "@") {
		return s.LoginByEmail(ctx, identifier, password, client)
	}
	return s.Login(ctx, identifier, password, client)
}

func (s *Service) passwordSignIn(ctx context.Context, user *User, lookupErr error, password string, client Client) (*TokenPair, error) {
	if lookupErr != nil {
		if errors.Is(lookupErr, ErrUserNotFound) {
			_, _ = compareHash(ctx, password, decoyHash)
			return nil, ErrInvalidCredentials
		}
		return nil, lookupErr
	}

	stored, err := s.repo.PasswordOf(ctx, user.ID)
	if err != nil {
		if errors.Is(err, ErrNoPassword) {
			_, _ = compareHash(ctx, password, decoyHash)
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}
	ok, err := compareHash(ctx, password, stored.Hash)
	if err != nil {
		return nil, fmt.Errorf("verify password: %w", err)
	}
	if !ok {
		return nil, ErrInvalidCredentials
	}
	return s.issuePair(ctx, user, uuid.New(), client)
}

func (s *Service) ForgotPassword(ctx context.Context, rawEmail string) error {
	email, err := NormalizeEmail(rawEmail)
	if err != nil {
		return err
	}
	now := time.Now()
	key := PurposeReset + ":" + email
	if soon, err := s.cooldownActive(ctx, key, now); err != nil {
		return err
	} else if soon {
		return ErrCodeTooSoon
	}

	record := &EmailCode{Key: key, Purpose: PurposeReset, Email: email, ExpiresAt: now.Add(codeTTL), CreatedAt: now}
	user, err := s.repo.UserByEmail(ctx, email)
	if errors.Is(err, ErrUserNotFound) {
		return s.repo.SaveEmailCode(ctx, record)
	}
	if err != nil {
		return err
	}
	record.UserID = &user.ID
	return s.issueCode(ctx, record,
		"Сброс пароля LifeLog",
		"Код для сброса пароля: {code}\n\nОн действует 15 минут. Если вы не запрашивали сброс, просто проигнорируйте это письмо — пароль не изменится.",
		true)
}

func (s *Service) ResetPasswordWithCode(ctx context.Context, rawEmail, code, next string) error {
	if len([]rune(next)) < minPasswordLength {
		return ErrWeakPassword
	}
	email, err := NormalizeEmail(rawEmail)
	if err != nil {
		return ErrCodeInvalid
	}
	key := PurposeReset + ":" + email

	record, err := s.checkCode(ctx, key, code, time.Now())
	if err != nil {
		return err
	}
	if record.UserID == nil {
		return ErrCodeInvalid
	}
	if root, err := s.isRoot(ctx, *record.UserID); err != nil {
		return err
	} else if root {
		return ErrManagedPassword
	}
	hash, err := createHash(ctx, next)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	return s.repo.ResetPasswordWithCode(ctx, key, record.CodeHash, *record.UserID, hash)
}

func (s *Service) AttachEmailStart(ctx context.Context, userID uuid.UUID, rawEmail string) error {
	email, err := NormalizeEmail(rawEmail)
	if err != nil {
		return err
	}

	now := time.Now()
	hits, err := s.repo.HitRateCounter(ctx, attachCounterPrefix+userID.String(), now, attachWindow)
	if err != nil {
		return err
	}
	if hits > maxAttachPerHour {
		return ErrAttachLimit
	}
	key := PurposeAttach + ":" + userID.String()
	if soon, err := s.cooldownActive(ctx, key, now); err != nil {
		return err
	} else if soon {
		return ErrCodeTooSoon
	}

	if owner, err := s.repo.UserByEmail(ctx, email); err == nil && owner.ID != userID {
		return ErrEmailTaken
	} else if err != nil && !errors.Is(err, ErrUserNotFound) {
		return err
	}

	record := &EmailCode{Key: key, Purpose: PurposeAttach, Email: email, UserID: &userID, ExpiresAt: now.Add(codeTTL), CreatedAt: now}
	return s.issueCode(ctx, record,
		"Подтверждение email в LifeLog",
		"Код подтверждения адреса: {code}\n\nОн действует 15 минут. Если вы не добавляли этот адрес в LifeLog, просто проигнорируйте письмо.",
		true)
}

func (s *Service) AttachEmailConfirm(ctx context.Context, userID uuid.UUID, code string) error {
	key := PurposeAttach + ":" + userID.String()
	record, err := s.checkCode(ctx, key, code, time.Now())
	if err != nil {
		return err
	}
	return s.repo.AttachEmailWithCode(ctx, key, record.CodeHash, userID, record.Email)
}
