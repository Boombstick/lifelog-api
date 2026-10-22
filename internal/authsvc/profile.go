package authsvc

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

const maxDisplayNameLength = 64

var (
	ErrDisplayNameLength  = errors.New("Имя должно быть от 1 до 64 символов")
	ErrDisplayNameControl = errors.New("Имя не должно содержать управляющих и невидимых символов")
)

const maxLoginLength = 64

var (
	ErrLoginHasAt   = errors.New("Логин не может содержать @")
	ErrLoginLength  = errors.New("Логин должен быть не длиннее 64 символов")
	ErrLoginSymbols = errors.New("Логин не должен содержать пробелов, управляющих и невидимых символов")
)

func NormalizeLogin(raw string) (string, error) {
	if !utf8.ValidString(raw) {
		return "", ErrLoginSymbols
	}
	login := normalizeLogin(raw)
	if login == "" {
		return "", ErrLoginRequired
	}
	if strings.Contains(login, "@") {
		return "", ErrLoginHasAt
	}
	for _, r := range login {
		if unicode.IsSpace(r) || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return "", ErrLoginSymbols
		}
	}
	if utf8.RuneCountInString(login) > maxLoginLength {
		return "", ErrLoginLength
	}
	return login, nil
}

const zeroWidthJoiner = rune(0x200d)

func NormalizeDisplayName(raw string) (string, error) {
	if !utf8.ValidString(raw) {
		return "", ErrDisplayNameControl
	}
	for _, r := range raw {
		if unicode.IsControl(r) && !unicode.IsSpace(r) {
			return "", ErrDisplayNameControl
		}
		if unicode.Is(unicode.Cf, r) && r != zeroWidthJoiner {
			return "", ErrDisplayNameControl
		}
	}
	name := strings.Join(strings.Fields(raw), " ")
	if n := utf8.RuneCountInString(name); n < 1 || n > maxDisplayNameLength {
		return "", ErrDisplayNameLength
	}
	return name, nil
}

func (s *Service) SetDisplayName(ctx context.Context, userID uuid.UUID, raw string) (*User, error) {
	name, err := NormalizeDisplayName(raw)
	if err != nil {
		return nil, err
	}
	if err := s.repo.SetDisplayName(ctx, userID, name); err != nil {
		return nil, err
	}
	return s.repo.UserByID(ctx, userID)
}
