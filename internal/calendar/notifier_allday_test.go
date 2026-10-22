package calendar

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func runAt(t *testing.T, now time.Time, repo *fakeNotifierRepo, user uuid.UUID) []sentMessage {
	t.Helper()
	n, messenger := buildNotifier(now, repo, &fakeCatalog{}, fakeBook{user: 1})
	if err := n.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	return messenger.sent
}

func allDayRepo(user uuid.UUID, zone, digestAt string, lead int, day time.Time) *fakeNotifierRepo {
	repo := newFakeRepo(Settings{UserID: user, DigestAt: digestAt, Timezone: zone, Enabled: true, LeadMinutes: lead})
	repo.events = []Event{{ID: uuid.New(), UserID: user, Title: "Отпуск", AllDay: true, StartsAt: day}}
	return repo
}

func TestAllDayReminderCountsBackFromLocalMidnight(t *testing.T) {
	t.Parallel()

	day := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		zone     string
		lead     int
		fires    time.Time
		oldFired time.Time
	}{
		{"Asia/Bangkok", 30, time.Date(2026, 10, 19, 16, 30, 0, 0, time.UTC), time.Date(2026, 10, 19, 23, 40, 0, 0, time.UTC)},

		{"Europe/Moscow", 60, time.Date(2026, 10, 19, 20, 0, 0, 0, time.UTC), time.Date(2026, 10, 19, 23, 10, 0, 0, time.UTC)},

		{"America/New_York", 30, time.Date(2026, 10, 20, 3, 30, 0, 0, time.UTC), time.Date(2026, 10, 19, 23, 30, 0, 0, time.UTC)},
	}
	for _, c := range cases {
		t.Run(c.zone, func(t *testing.T) {
			t.Parallel()
			user := uuid.New()

			if sent := runAt(t, c.fires.Add(-time.Minute), allDayRepo(user, c.zone, "23:59", c.lead, day), user); len(sent) != 0 {
				t.Errorf("a minute early must stay silent: %+v", sent)
			}
			if sent := runAt(t, c.oldFired, allDayRepo(user, c.zone, "23:59", c.lead, day), user); len(sent) != 0 {
				t.Errorf("the old UTC-midnight moment must stay silent: %+v", sent)
			}
			sent := runAt(t, c.fires, allDayRepo(user, c.zone, "23:59", c.lead, day), user)
			if len(sent) != 1 {
				t.Fatalf("want one reminder, got %+v", sent)
			}
			if !strings.Contains(sent[0].text, "Завтра весь день") || !strings.Contains(sent[0].text, "Отпуск") {
				t.Errorf("unexpected text %q", sent[0].text)
			}
			if strings.Contains(sent[0].text, ":00") {
				t.Errorf("an all-day reminder must not name a clock time: %q", sent[0].text)
			}
		})
	}
}

func TestAllDayLeadNamesTheDay(t *testing.T) {
	t.Parallel()

	bangkok, _ := time.LoadLocation("Asia/Bangkok")
	entry := Entry{AllDay: true, When: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)}
	for now, want := range map[time.Time]string{
		time.Date(2026, 10, 20, 8, 0, 0, 0, bangkok):  "Сегодня весь день",
		time.Date(2026, 10, 19, 23, 0, 0, 0, bangkok): "Завтра весь день",
		time.Date(2026, 10, 18, 9, 0, 0, 0, bangkok):  "Послезавтра весь день",
		time.Date(2026, 10, 15, 9, 0, 0, 0, bangkok):  "Через 5 дн., весь день",
	} {
		if got := reminderLead(entry, bangkok, 30, now.UTC()); got != want {
			t.Errorf("at %v: %q, want %q", now, got, want)
		}
	}
}

func TestEveningDigestWestOfGreenwichListsAllDayEntries(t *testing.T) {
	t.Parallel()

	user := uuid.New()
	repo := allDayRepo(user, "America/New_York", "21:00", 0, time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC))
	sent := runAt(t, time.Date(2026, 11, 17, 2, 0, 0, 0, time.UTC), repo, user)
	if len(sent) != 1 || !strings.Contains(sent[0].text, "Отпуск") {
		t.Fatalf("the evening digest must list the day's all-day entry: %+v", sent)
	}
}

func TestDigestListsAllDayEntriesEastOfGreenwich(t *testing.T) {
	t.Parallel()

	day := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	for zone, now := range map[string]time.Time{
		"Asia/Bangkok":       time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC),
		"Europe/Moscow":      time.Date(2026, 10, 20, 6, 0, 0, 0, time.UTC),
		"Pacific/Kiritimati": time.Date(2026, 10, 19, 19, 0, 0, 0, time.UTC),
	} {
		user := uuid.New()
		sent := runAt(t, now, allDayRepo(user, zone, "09:00", 0, day), user)
		if len(sent) != 1 || !strings.Contains(sent[0].text, "Отпуск") {
			t.Errorf("%s: the morning digest must list the day's all-day entry: %+v", zone, sent)
		}
	}
}

func TestDigestIsNotSentLongAfterItsMinute(t *testing.T) {
	t.Parallel()

	user := uuid.New()
	repo := newFakeRepo(Settings{UserID: user, DigestAt: "09:00", Timezone: "UTC", Enabled: true})
	repo.events = []Event{{ID: uuid.New(), UserID: user, Title: "Встреча", StartsAt: time.Date(2026, 9, 16, 18, 0, 0, 0, time.UTC)}}

	if sent := runAt(t, time.Date(2026, 9, 16, 15, 0, 0, 0, time.UTC), repo, user); len(sent) != 0 {
		t.Errorf("six hours late is not a digest: %+v", sent)
	}
	if sent := runAt(t, time.Date(2026, 9, 16, 11, 59, 0, 0, time.UTC), repo, user); len(sent) != 1 {
		t.Errorf("within three hours the digest still goes: %+v", sent)
	}
}

func TestZoneChangeDoesNotResendADigest(t *testing.T) {
	t.Parallel()

	user := uuid.New()
	repo := newFakeRepo(Settings{UserID: user, DigestAt: "09:00", Timezone: "Asia/Bangkok", Enabled: true})
	repo.events = []Event{
		{ID: uuid.New(), UserID: user, Title: "Вчера", AllDay: true, StartsAt: time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)},
		{ID: uuid.New(), UserID: user, Title: "Сегодня", AllDay: true, StartsAt: time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)},
	}
	if sent := runAt(t, time.Date(2026, 10, 16, 2, 0, 0, 0, time.UTC), repo, user); len(sent) != 1 {
		t.Fatalf("Bangkok digest: %+v", sent)
	}

	repo.settings[0].Timezone = "America/New_York"
	if sent := runAt(t, time.Date(2026, 10, 16, 2, 30, 0, 0, time.UTC), repo, user); len(sent) != 0 {
		t.Errorf("the 15th's digest time has long passed in New York: %+v", sent)
	}
}

func TestDefaultSettingsNotify(t *testing.T) {
	t.Parallel()

	user := uuid.New()
	repo := newFakeRepo(DefaultSettings(user))
	start := time.Date(2026, 9, 16, 18, 0, 0, 0, time.UTC)
	repo.events = []Event{{ID: uuid.New(), UserID: user, Title: "Встреча", StartsAt: start}}

	if sent := runAt(t, time.Date(2026, 9, 16, 9, 0, 0, 0, time.UTC), repo, user); len(sent) != 1 || !strings.HasPrefix(sent[0].text, "Сегодня") {
		t.Errorf("default digest at 09:00 UTC: %+v", sent)
	}
	if sent := runAt(t, start.Add(-30*time.Minute), repo, user); len(sent) != 1 || !strings.Contains(sent[0].text, "Через 30 мин.") {
		t.Errorf("default 30-minute reminder: %+v", sent)
	}
}
