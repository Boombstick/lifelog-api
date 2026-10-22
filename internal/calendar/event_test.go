package calendar

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCreateEventRejectsWhatCannotBeShown(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	before := start.Add(-time.Minute)

	cases := []struct {
		name  string
		event Event
		want  error
	}{
		{"blank title", Event{Title: "   ", StartsAt: start}, ErrTitleNeeded},
		{"no start", Event{Title: "Врач"}, ErrBadTime},
		{"end before start", Event{Title: "Врач", StartsAt: start, EndsAt: &before}, ErrBadTime},
		{"unknown zone", Event{Title: "Врач", StartsAt: start, Timezone: "Mars/Olympus"}, ErrBadTimezone},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			service, _ := newSettingsService()
			event := c.event
			event.UserID = uuid.New()
			if err := service.CreateEvent(context.Background(), &event); !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if event.ID != uuid.Nil {
				t.Error("a rejected event must not be given an id")
			}
		})
	}
}

func TestCreateEventAcceptsAValidSpan(t *testing.T) {
	t.Parallel()

	service, repo := newSettingsService()
	start := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	event := Event{UserID: uuid.New(), Title: "  Врач  ", StartsAt: start, EndsAt: &end, Timezone: "Asia/Bangkok"}

	if err := service.CreateEvent(context.Background(), &event); err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}
	if event.Title != "Врач" {
		t.Errorf("title = %q, want it trimmed", event.Title)
	}
	if event.ID == uuid.Nil {
		t.Error("a stored event needs an id")
	}
	if repo.created != 1 {
		t.Errorf("repository writes = %d, want 1", repo.created)
	}

	event.EndsAt = &start
	if err := service.UpdateEvent(context.Background(), &event); err != nil {
		t.Errorf("an end equal to the start is a moment, not an error: %v", err)
	}
	backwards := start.Add(-time.Minute)
	event.EndsAt = &backwards
	if err := service.UpdateEvent(context.Background(), &event); !errors.Is(err, ErrBadTime) {
		t.Errorf("update err = %v, want ErrBadTime", err)
	}
}

func TestAllDayEventIsStoredAtMidnightUTCOfTheWrittenDate(t *testing.T) {
	t.Parallel()

	for _, zone := range []string{"Asia/Bangkok", "Europe/Moscow", "America/New_York"} {
		t.Run(zone, func(t *testing.T) {
			location, err := time.LoadLocation(zone)
			if err != nil {
				t.Fatal(err)
			}
			service, _ := newSettingsService()
			start := time.Date(2026, 10, 20, 0, 0, 0, 0, location)
			end := time.Date(2026, 10, 22, 0, 0, 0, 0, location)
			event := Event{UserID: uuid.New(), Title: "Отпуск", AllDay: true, StartsAt: start, EndsAt: &end}
			if err := service.CreateEvent(context.Background(), &event); err != nil {
				t.Fatal(err)
			}
			if want := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC); !event.StartsAt.Equal(want) {
				t.Errorf("start = %v, want %v", event.StartsAt, want)
			}
			if want := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC); event.EndsAt == nil || !event.EndsAt.Equal(want) {
				t.Errorf("end = %v, want %v", event.EndsAt, want)
			}
			if !event.WireStart().Equal(event.StartsAt) {
				t.Errorf("the event DTO and the agenda must agree: %v vs %v", event.WireStart(), event.StartsAt)
			}
		})
	}
}

func TestTimedEventKeepsItsInstant(t *testing.T) {
	t.Parallel()

	bangkok, _ := time.LoadLocation("Asia/Bangkok")
	service, _ := newSettingsService()
	start := time.Date(2026, 10, 20, 19, 0, 0, 0, bangkok)
	event := Event{UserID: uuid.New(), Title: "Ужин", StartsAt: start}
	if err := service.CreateEvent(context.Background(), &event); err != nil {
		t.Fatal(err)
	}
	if !event.StartsAt.Equal(start) {
		t.Errorf("start = %v, want %v", event.StartsAt, start)
	}
}
