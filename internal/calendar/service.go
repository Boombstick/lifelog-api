package calendar

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Repository interface {
	CreateEvent(ctx context.Context, event *Event) error
	UpdateEvent(ctx context.Context, event *Event) error
	DeleteEvent(ctx context.Context, userID, id uuid.UUID) error
	Event(ctx context.Context, userID, id uuid.UUID) (*Event, error)
	EventsBetween(ctx context.Context, userID uuid.UUID, from, to time.Time) ([]Event, error)
	ImportedBetween(ctx context.Context, userID uuid.UUID, from, to time.Time) ([]ImportedEvent, error)

	Settings(ctx context.Context, userID uuid.UUID) (*Settings, error)
	SaveSettings(ctx context.Context, settings *Settings) error
}

type CatalogSource interface {
	Upcoming(ctx context.Context, from, to time.Time, user uuid.UUID) ([]CatalogEvent, error)
}

type CatalogEvent struct {
	UserID       uuid.UUID `json:"user_id"`
	Kind         Kind      `json:"kind"`
	FilmID       uint      `json:"film_id"`
	Title        string    `json:"title"`
	Date         time.Time `json:"date"`
	SeasonNumber *int      `json:"season_number,omitempty"`
	PosterURL    string    `json:"poster_url,omitempty"`
}

type Entry struct {
	Kind Kind      `json:"kind"`
	When time.Time `json:"when"`

	EndsAt *time.Time `json:"ends_at,omitempty"`

	AllDay   bool       `json:"all_day"`
	Title    string     `json:"title"`
	Details  string     `json:"details,omitempty"`
	Location string     `json:"location,omitempty"`
	EventID  *uuid.UUID `json:"event_id,omitempty"`
	FilmID   *uint      `json:"film_id,omitempty"`
	Season   *int       `json:"season_number,omitempty"`
	Poster   string     `json:"poster_url,omitempty"`

	Timezone string `json:"timezone,omitempty"`
	GoogleID string `json:"google_event_id,omitempty"`
	Link     string `json:"link,omitempty"`
}

func (e Entry) Ref() string {
	switch {
	case e.GoogleID != "":

		sum := sha256.Sum256([]byte(e.GoogleID))
		return "google:" + hex.EncodeToString(sum[:])
	case e.EventID != nil:
		return "event:" + e.EventID.String()
	case e.FilmID != nil && e.Season != nil:
		return fmt.Sprintf("season:%d:%d", *e.FilmID, *e.Season)
	case e.FilmID != nil:
		return fmt.Sprintf("film:%d", *e.FilmID)
	}
	return "unknown:" + e.Title
}

const MaxLeadMinutes = 30 * 24 * 60

func DefaultSettings(userID uuid.UUID) Settings {
	return Settings{UserID: userID, DigestAt: "09:00", Timezone: "UTC", Enabled: true, LeadMinutes: 30}
}

type Service struct {
	repo    Repository
	catalog CatalogSource
	logger  *slog.Logger
}

func NewService(repo Repository, catalog CatalogSource, logger *slog.Logger) *Service {
	return &Service{repo: repo, catalog: catalog, logger: logger}
}

func (s *Service) CreateEvent(ctx context.Context, event *Event) error {
	if err := validateEvent(event); err != nil {
		return err
	}
	event.ID = uuid.New()
	return s.repo.CreateEvent(ctx, event)
}

func (s *Service) UpdateEvent(ctx context.Context, event *Event) error {
	if err := validateEvent(event); err != nil {
		return err
	}
	return s.repo.UpdateEvent(ctx, event)
}

func (s *Service) DeleteEvent(ctx context.Context, userID, id uuid.UUID) error {
	return s.repo.DeleteEvent(ctx, userID, id)
}

func (s *Service) Event(ctx context.Context, userID, id uuid.UUID) (*Event, error) {
	return s.repo.Event(ctx, userID, id)
}

func validateEvent(event *Event) error {
	event.Title = strings.TrimSpace(event.Title)
	if event.Title == "" {
		return ErrTitleNeeded
	}
	if event.StartsAt.IsZero() {
		return ErrBadTime
	}
	if event.AllDay {
		event.StartsAt = dateAsWritten(event.StartsAt)
		if event.EndsAt != nil {
			end := dateAsWritten(*event.EndsAt)
			event.EndsAt = &end
		}
	}
	if event.EndsAt != nil && event.EndsAt.Before(event.StartsAt) {
		return ErrBadTime
	}

	if event.Timezone != "" {
		if !validZone(event.Timezone) {
			return fmt.Errorf("%w: %s", ErrBadTimezone, event.Timezone)
		}
	}
	return nil
}

func (s *Service) Agenda(ctx context.Context, userID uuid.UUID, from, to time.Time) ([]Entry, error) {
	events, err := s.repo.EventsBetween(ctx, userID, from, to)
	if err != nil {
		return nil, fmt.Errorf("own events: %w", err)
	}

	entries := make([]Entry, 0, len(events))
	for _, event := range events {
		id := event.ID
		entries = append(entries, Entry{
			Kind:     KindOwnEvent,
			When:     wireTime(event.StartsAt, event.AllDay),
			EndsAt:   wireEnd(event.EndsAt, event.AllDay),
			AllDay:   event.AllDay,
			Title:    event.Title,
			Details:  event.Details,
			Location: event.Location,
			Timezone: event.Timezone,
			EventID:  &id,
		})
	}

	imported, err := s.repo.ImportedBetween(ctx, userID, from, to)
	if err != nil {
		return nil, fmt.Errorf("imported events: %w", err)
	}
	for _, event := range imported {
		entries = append(entries, entryFromImported(event))
	}

	if s.catalog != nil {
		catalogEvents, err := s.catalog.Upcoming(ctx, from, to, userID)
		if err != nil {
			s.logger.Warn("catalogue unavailable, agenda has own events only", "error", err)
		} else {
			for _, item := range catalogEvents {
				if item.UserID != userID {
					continue
				}
				entries = append(entries, entryFromCatalog(item))
			}
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].When.Equal(entries[j].When) {
			return entries[i].Title < entries[j].Title
		}
		return entries[i].When.Before(entries[j].When)
	})
	return entries, nil
}

func entryFromImported(event ImportedEvent) Entry {
	return Entry{
		Kind:     KindGoogleEvent,
		When:     wireTime(event.StartsAt, event.AllDay),
		EndsAt:   wireEnd(event.EndsAt, event.AllDay),
		AllDay:   event.AllDay,
		Title:    event.Title,
		Details:  event.Details,
		Location: event.Location,
		GoogleID: event.GoogleID,
		Link:     event.Link,
	}
}

func entryFromCatalog(item CatalogEvent) Entry {
	filmID := item.FilmID
	entry := Entry{
		Kind:   item.Kind,
		When:   wireTime(item.Date, true),
		AllDay: true,
		Title:  item.Title,
		FilmID: &filmID,
		Poster: item.PosterURL,
	}
	if item.SeasonNumber != nil {
		season := *item.SeasonNumber
		entry.Season = &season
	}
	return entry
}

func (s *Service) SettingsOf(ctx context.Context, userID uuid.UUID) (Settings, error) {
	settings, err := s.repo.Settings(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return DefaultSettings(userID), nil
		}
		return Settings{}, err
	}
	return *settings, nil
}

func (s *Service) SaveSettings(ctx context.Context, settings *Settings) error {
	if _, _, err := ParseDigestAt(settings.DigestAt); err != nil {
		return err
	}
	if settings.LeadMinutes < 0 || settings.LeadMinutes > MaxLeadMinutes {
		return ErrBadLead
	}
	if settings.Timezone == "" {
		settings.Timezone = "UTC"
	}
	if !validZone(settings.Timezone) {
		return fmt.Errorf("calendar: unknown timezone %q", settings.Timezone)
	}
	return s.repo.SaveSettings(ctx, settings)
}

func ParseDigestAt(value string) (hour, minute int, err error) {
	parsed, err := time.Parse("15:04", value)
	if err != nil {
		return 0, 0, ErrBadDigest
	}
	return parsed.Hour(), parsed.Minute(), nil
}

func wireEnd(when *time.Time, allDay bool) *time.Time {
	if when == nil {
		return nil
	}
	out := wireTime(*when, allDay)
	return &out
}

func wireTime(when time.Time, allDay bool) time.Time {
	if !allDay {
		return when.UTC()
	}
	return dateAsWritten(when.UTC())
}

func dateAsWritten(when time.Time) time.Time {
	year, month, day := when.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func (e Event) WireStart() time.Time { return wireTime(e.StartsAt, e.AllDay) }
func (e Event) WireEnd() *time.Time  { return wireEnd(e.EndsAt, e.AllDay) }

func validZone(name string) bool {
	if name == "" || name == "Local" {
		return false
	}
	_, err := time.LoadLocation(name)
	return err == nil
}
