package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"lifelog-api/internal/auth"
	"lifelog-api/internal/film"
	"lifelog-api/internal/model"
	"lifelog-api/internal/randtoken"
)

var (
	ErrShareTargetMissing = errors.New("нужен набор или фильм")
	ErrShareTTLOutOfRange = errors.New("срок жизни ссылки от 1 минуты до 2 суток")
)

const (
	MaxShareTTL = 48 * time.Hour

	DefaultShareTTL = 24 * time.Hour
)

func shareTTL(minutes int) (time.Duration, error) {
	if minutes == 0 {
		return DefaultShareTTL, nil
	}
	ttl := time.Duration(minutes) * time.Minute
	if ttl <= 0 || ttl > MaxShareTTL {
		return 0, ErrShareTTLOutOfRange
	}
	return ttl, nil
}

type SharedList struct {
	Owner struct {
		DisplayName string `json:"display_name"`
	} `json:"owner"`
	Set        string       `json:"set"`
	Items      []MyFilmItem `json:"items"`
	Total      int          `json:"total"`
	NextCursor string       `json:"next_cursor,omitempty"`

	ExpiresAt time.Time `json:"expires_at"`
}

type ShareLinkView struct {
	Token     string `json:"token"`
	URL       string `json:"url"`
	Set       string `json:"set"`
	FilmID    *uint  `json:"film_id"`
	CreatedAt string `json:"created_at"`
	ExpiresAt string `json:"expires_at"`
}

func (s *SharingService) CreateShareLink(ctx context.Context, userID uint, set string, filmID *uint, ttlMinutes int) (*ShareLinkView, error) {
	if filmID == nil {
		entrySet := film.EntrySet(set)
		if set == "" || !entrySet.Valid() {
			return nil, ErrShareTargetMissing
		}
	} else {
		set = ""
	}

	ttl, err := shareTTL(ttlMinutes)
	if err != nil {
		return nil, err
	}

	token := randtoken.New(shareTokenBytes)

	now := time.Now()
	link := &auth.ShareLink{
		Token:     token,
		UserID:    userID,
		Set:       set,
		FilmID:    filmID,
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
	}
	if err := s.repo.CreateShareLink(ctx, link); err != nil {
		return nil, err
	}
	return s.viewOf(link), nil
}

func (s *SharingService) ShareLinks(ctx context.Context, userID uint) ([]ShareLinkView, error) {
	links, err := s.repo.ShareLinksOf(ctx, userID)
	if err != nil {
		return nil, err
	}

	views := make([]ShareLinkView, 0, len(links))
	for i := range links {
		views = append(views, *s.viewOf(&links[i]))
	}
	return views, nil
}

func (s *SharingService) RevokeShareLink(ctx context.Context, userID uint, token string) error {
	return s.repo.RevokeShareLink(ctx, userID, token)
}

func (s *SharingService) SharedList(ctx context.Context, token string, limit int, cursor string) (*SharedList, error) {
	if !validToken(token) {
		return nil, model.NewEntityNotFound("share link")
	}

	link, err := s.repo.ShareLinkByToken(ctx, token)
	if err != nil {
		return nil, err
	}

	owner, err := s.identity.FindByID(ctx, link.UserID)
	if err != nil {
		return nil, err
	}

	result := &SharedList{Set: link.Set, ExpiresAt: link.ExpiresAt}
	result.Owner.DisplayName = owner.Username

	if link.FilmID != nil {
		entry, err := s.films.SharedEntry(ctx, link.UserID, *link.FilmID)
		if err != nil {
			return nil, err
		}
		result.Items = []MyFilmItem{*entry}
		result.Total = 1
		return result, nil
	}

	page, err := s.films.MyFilms(ctx, link.UserID, MyFilmsRequest{Set: link.Set, Limit: limit, Cursor: cursor})
	if err != nil {
		return nil, err
	}
	result.Items = page.Items
	result.Total = page.Total
	result.NextCursor = page.NextCursor
	return result, nil
}

func (s *SharingService) viewOf(link *auth.ShareLink) *ShareLinkView {
	return &ShareLinkView{
		Token:     link.Token,
		URL:       fmt.Sprintf("%s/s/%s", s.publicURL, link.Token),
		Set:       link.Set,
		FilmID:    link.FilmID,
		CreatedAt: link.CreatedAt.Format(time.RFC3339),
		ExpiresAt: link.ExpiresAt.Format(time.RFC3339),
	}
}

const shareTokenBytes = 32

func validToken(token string) bool {
	if len(token) != 43 {
		return false
	}
	for _, symbol := range token {
		switch {
		case symbol >= 'A' && symbol <= 'Z',
			symbol >= 'a' && symbol <= 'z',
			symbol >= '0' && symbol <= '9',
			symbol == '-', symbol == '_':
		default:
			return false
		}
	}
	return true
}
