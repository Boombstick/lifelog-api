package calendar

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeSettingsRepo struct {
	stored *Settings

	created int
}

func (f *fakeSettingsRepo) CreateEvent(context.Context, *Event) error {
	f.created++
	return nil
}
func (f *fakeSettingsRepo) UpdateEvent(context.Context, *Event) error               { return nil }
func (f *fakeSettingsRepo) DeleteEvent(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (f *fakeSettingsRepo) Event(context.Context, uuid.UUID, uuid.UUID) (*Event, error) {
	return nil, ErrNotFound
}
func (f *fakeSettingsRepo) EventsBetween(context.Context, uuid.UUID, time.Time, time.Time) ([]Event, error) {
	return nil, nil
}
func (f *fakeSettingsRepo) ImportedBetween(context.Context, uuid.UUID, time.Time, time.Time) ([]ImportedEvent, error) {
	return nil, nil
}
func (f *fakeSettingsRepo) Settings(_ context.Context, userID uuid.UUID) (*Settings, error) {
	if f.stored == nil || f.stored.UserID != userID {
		return nil, ErrNotFound
	}
	copied := *f.stored
	return &copied, nil
}
func (f *fakeSettingsRepo) SaveSettings(_ context.Context, settings *Settings) error {
	copied := *settings
	f.stored = &copied
	return nil
}

func newSettingsService() (*Service, *fakeSettingsRepo) {
	repo := &fakeSettingsRepo{}
	return NewService(repo, nil, quiet()), repo
}

func TestSettingsOfFallsBackToDefaults(t *testing.T) {
	t.Parallel()

	service, _ := newSettingsService()
	user := uuid.New()

	settings, err := service.SettingsOf(context.Background(), user)
	if err != nil {
		t.Fatal(err)
	}
	if settings != DefaultSettings(user) {
		t.Errorf("an untouched account must read as the defaults, got %+v", settings)
	}
}

func TestSwitchingNotificationsOffSurvivesASave(t *testing.T) {
	t.Parallel()

	service, _ := newSettingsService()
	user := uuid.New()

	settings := DefaultSettings(user)
	settings.Enabled = false
	settings.LeadMinutes = 0
	if err := service.SaveSettings(context.Background(), &settings); err != nil {
		t.Fatal(err)
	}

	read, err := service.SettingsOf(context.Background(), user)
	if err != nil {
		t.Fatal(err)
	}
	if read.Enabled {
		t.Error("notifications were switched off and came back on")
	}
	if read.LeadMinutes != 0 {
		t.Errorf("a zero lead came back as %d", read.LeadMinutes)
	}
}

func TestSaveSettingsRejectsWhatCannotWork(t *testing.T) {
	t.Parallel()

	user := uuid.New()
	cases := []struct {
		name     string
		settings Settings
		want     error
	}{
		{
			name:     "lead beyond a month",
			settings: Settings{UserID: user, DigestAt: "09:00", Timezone: "UTC", LeadMinutes: MaxLeadMinutes + 1},
			want:     ErrBadLead,
		},
		{
			name:     "negative lead",
			settings: Settings{UserID: user, DigestAt: "09:00", Timezone: "UTC", LeadMinutes: -1},
			want:     ErrBadLead,
		},
		{
			name:     "digest time that is not a time",
			settings: Settings{UserID: user, DigestAt: "25:00", Timezone: "UTC"},
			want:     ErrBadDigest,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			service, repo := newSettingsService()
			settings := tc.settings
			if err := service.SaveSettings(context.Background(), &settings); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, expected %v", err, tc.want)
			}
			if repo.stored != nil {
				t.Error("a rejected value reached storage anyway")
			}
		})
	}
}

func TestSaveSettingsRejectsAnUnknownZone(t *testing.T) {
	t.Parallel()

	service, repo := newSettingsService()
	settings := Settings{UserID: uuid.New(), DigestAt: "09:00", Timezone: "Mars/Olympus"}
	if err := service.SaveSettings(context.Background(), &settings); err == nil {
		t.Fatal("an unknown zone was accepted")
	}
	if repo.stored != nil {
		t.Error("an unknown zone reached storage anyway")
	}
}

func TestSettingsCarryNoGormDefaultsOnSwitchableFields(t *testing.T) {
	t.Parallel()

	typ := reflect.TypeOf(Settings{})
	for _, name := range []string{"Enabled", "LeadMinutes"} {
		field, ok := typ.FieldByName(name)
		if !ok {
			t.Fatalf("Settings has no field %s", name)
		}
		if strings.Contains(field.Tag.Get("gorm"), "default:") {
			t.Errorf("%s carries a gorm default; switching it off would silently do nothing", name)
		}
	}
}
