package handler

import (
	"time"

	"lifelog-api/internal/calendar"

	"github.com/google/uuid"
)

type eventDTO struct {
	ID        uuid.UUID  `json:"id"`
	Title     string     `json:"title"`
	Details   string     `json:"details,omitempty"`
	Location  string     `json:"location,omitempty"`
	StartsAt  time.Time  `json:"starts_at"`
	EndsAt    *time.Time `json:"ends_at,omitempty"`
	AllDay    bool       `json:"all_day"`
	Timezone  string     `json:"timezone,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

func newEventDTO(event *calendar.Event) eventDTO {
	return eventDTO{
		ID:        event.ID,
		Title:     event.Title,
		Details:   event.Details,
		Location:  event.Location,
		StartsAt:  event.WireStart(),
		EndsAt:    event.WireEnd(),
		AllDay:    event.AllDay,
		Timezone:  event.Timezone,
		CreatedAt: event.CreatedAt.UTC(),
		UpdatedAt: event.UpdatedAt.UTC(),
	}
}

type settingsDTO struct {
	DigestAt    string `json:"digest_at"`
	Timezone    string `json:"timezone"`
	Enabled     bool   `json:"enabled"`
	LeadMinutes int    `json:"lead_minutes"`
}

func newSettingsDTO(settings calendar.Settings) settingsDTO {
	return settingsDTO{
		DigestAt:    settings.DigestAt,
		Timezone:    settings.Timezone,
		Enabled:     settings.Enabled,
		LeadMinutes: settings.LeadMinutes,
	}
}
