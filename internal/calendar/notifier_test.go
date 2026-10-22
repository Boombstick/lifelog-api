package calendar

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type sentMessage struct {
	chatID int64
	text   string
}

type fakeNotifierRepo struct {
	settings []Settings
	events   []Event
	imported []ImportedEvent

	delivered map[string]bool
	forgot    int
	released  int
}

func newFakeRepo(settings ...Settings) *fakeNotifierRepo {
	return &fakeNotifierRepo{
		settings:  settings,
		delivered: map[string]bool{},
	}
}

func (f *fakeNotifierRepo) AllSettings(context.Context) ([]Settings, error) { return f.settings, nil }
func (f *fakeNotifierRepo) AllImportedBetween(_ context.Context, from, to time.Time) ([]ImportedEvent, error) {
	var out []ImportedEvent
	for _, event := range f.imported {
		if !event.StartsAt.Before(from) && event.StartsAt.Before(to) {
			out = append(out, event)
		}
	}
	return out, nil
}
func (f *fakeNotifierRepo) AllEventsBetween(_ context.Context, from, to time.Time) ([]Event, error) {
	var out []Event
	for _, event := range f.events {
		if !event.StartsAt.Before(from) && event.StartsAt.Before(to) {
			out = append(out, event)
		}
	}
	return out, nil
}
func (f *fakeNotifierRepo) MarkSent(_ context.Context, userID uuid.UUID, ref, slot string) (bool, error) {
	key := userID.String() + "|" + ref + "|" + slot
	if f.delivered[key] {
		return false, nil
	}
	f.delivered[key] = true
	return true, nil
}
func (f *fakeNotifierRepo) ForgetDelivery(_ context.Context, userID uuid.UUID, ref, slot string) error {
	delete(f.delivered, userID.String()+"|"+ref+"|"+slot)
	f.released++
	return nil
}
func (f *fakeNotifierRepo) ForgetDeliveries(context.Context, time.Time) (int64, error) {
	f.forgot++
	return 0, nil
}

type fakeCatalog struct {
	events []CatalogEvent
	err    error
}

func (f *fakeCatalog) Upcoming(_ context.Context, from, to time.Time, user uuid.UUID) ([]CatalogEvent, error) {
	if f.err != nil {
		return nil, f.err
	}
	var out []CatalogEvent
	for _, event := range f.events {
		if user != uuid.Nil && event.UserID != user {
			continue
		}
		if !event.Date.Before(from) && event.Date.Before(to) {
			out = append(out, event)
		}
	}
	return out, nil
}

type fakeBook map[uuid.UUID]int64

func (f fakeBook) TelegramIDs(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]int64, error) {
	out := map[uuid.UUID]int64{}
	for _, id := range ids {
		if chat, ok := f[id]; ok {
			out[id] = chat
		}
	}
	return out, nil
}

type fakeMessenger struct{ sent []sentMessage }

func (f *fakeMessenger) Send(_ context.Context, chatID int64, text string) error {
	f.sent = append(f.sent, sentMessage{chatID: chatID, text: text})
	return nil
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func buildNotifier(now time.Time, repo *fakeNotifierRepo, catalog CatalogSource, book fakeBook) (*Notifier, *fakeMessenger) {
	messenger := &fakeMessenger{}
	n := NewNotifier(repo, catalog, book, messenger, nil, quiet())
	n.now = func() time.Time { return now }
	return n, messenger
}

func TestDigestSendsOncePerDay(t *testing.T) {
	t.Parallel()

	user := uuid.New()
	repo := newFakeRepo(Settings{UserID: user, DigestAt: "09:00", Timezone: "UTC", Enabled: true})
	repo.events = []Event{{
		ID: uuid.New(), UserID: user, Title: "Стоматолог",
		StartsAt: time.Date(2026, 9, 16, 14, 0, 0, 0, time.UTC),
	}}
	now := time.Date(2026, 9, 16, 9, 0, 0, 0, time.UTC)
	n, messenger := buildNotifier(now, repo, &fakeCatalog{}, fakeBook{user: 555})

	if err := n.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(messenger.sent) != 1 {
		t.Fatalf("expected one digest, got %d", len(messenger.sent))
	}
	if !strings.Contains(messenger.sent[0].text, "Стоматолог") || messenger.sent[0].chatID != 555 {
		t.Errorf("unexpected digest %+v", messenger.sent[0])
	}

	if err := n.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(messenger.sent) != 1 {
		t.Errorf("digest repeated: %d messages", len(messenger.sent))
	}
}

func TestDigestStillSendsWhenLate(t *testing.T) {
	t.Parallel()

	user := uuid.New()
	repo := newFakeRepo(Settings{UserID: user, DigestAt: "09:00", Timezone: "UTC", Enabled: true})
	repo.events = []Event{{
		ID: uuid.New(), UserID: user, Title: "Созвон",
		StartsAt: time.Date(2026, 9, 16, 18, 0, 0, 0, time.UTC),
	}}
	now := time.Date(2026, 9, 16, 11, 30, 0, 0, time.UTC)
	n, messenger := buildNotifier(now, repo, &fakeCatalog{}, fakeBook{user: 7})

	if err := n.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(messenger.sent) != 1 {
		t.Fatalf("a late run must still send the digest, got %d", len(messenger.sent))
	}
}

func TestDigestWaitsForTheChosenTimeInTheUsersZone(t *testing.T) {
	t.Parallel()

	user := uuid.New()

	repo := newFakeRepo(Settings{UserID: user, DigestAt: "09:00", Timezone: "Asia/Bangkok", Enabled: true})
	repo.events = []Event{{
		ID: uuid.New(), UserID: user, Title: "Виза",
		StartsAt: time.Date(2026, 9, 16, 5, 0, 0, 0, time.UTC),
	}}

	early, messengerEarly := buildNotifier(time.Date(2026, 9, 16, 1, 0, 0, 0, time.UTC), repo, &fakeCatalog{}, fakeBook{user: 1})
	if err := early.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(messengerEarly.sent) != 0 {
		t.Errorf("08:00 in Bangkok is too early, sent %d", len(messengerEarly.sent))
	}

	later, messengerLater := buildNotifier(time.Date(2026, 9, 16, 3, 0, 0, 0, time.UTC), repo, &fakeCatalog{}, fakeBook{user: 1})
	if err := later.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(messengerLater.sent) != 1 {
		t.Errorf("10:00 in Bangkok should have sent, got %d", len(messengerLater.sent))
	}
}

func TestNoDigestOnAnEmptyDay(t *testing.T) {
	t.Parallel()

	user := uuid.New()
	repo := newFakeRepo(Settings{UserID: user, DigestAt: "09:00", Timezone: "UTC", Enabled: true})
	n, messenger := buildNotifier(time.Date(2026, 9, 16, 9, 0, 0, 0, time.UTC), repo, &fakeCatalog{}, fakeBook{user: 1})

	if err := n.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(messenger.sent) != 0 {
		t.Errorf("an empty day must stay silent, sent %d", len(messenger.sent))
	}
}

func TestReminderFiresOnceInsideItsWindow(t *testing.T) {
	t.Parallel()

	user := uuid.New()
	start := time.Date(2026, 9, 16, 15, 0, 0, 0, time.UTC)
	repo := newFakeRepo(Settings{UserID: user, DigestAt: "23:59", Timezone: "UTC", Enabled: true, LeadMinutes: 30})
	repo.events = []Event{{ID: uuid.New(), UserID: user, Title: "Врач", StartsAt: start}}

	early, messengerEarly := buildNotifier(start.Add(-40*time.Minute), repo, &fakeCatalog{}, fakeBook{user: 9})
	if err := early.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(messengerEarly.sent) != 0 {
		t.Fatalf("fired too early: %d", len(messengerEarly.sent))
	}

	n, messenger := buildNotifier(start.Add(-25*time.Minute), repo, &fakeCatalog{}, fakeBook{user: 9})
	if err := n.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(messenger.sent) != 1 || !strings.Contains(messenger.sent[0].text, "Врач") {
		t.Fatalf("expected one reminder, got %+v", messenger.sent)
	}

	if err := n.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(messenger.sent) != 1 {
		t.Errorf("reminder repeated: %d", len(messenger.sent))
	}
}

func TestReminderDoesNotFireAfterTheEvent(t *testing.T) {
	t.Parallel()

	user := uuid.New()
	start := time.Date(2026, 9, 16, 15, 0, 0, 0, time.UTC)
	repo := newFakeRepo(Settings{UserID: user, DigestAt: "23:59", Timezone: "UTC", Enabled: true, LeadMinutes: 30})
	repo.events = []Event{{ID: uuid.New(), UserID: user, Title: "Прошедшее", StartsAt: start}}

	n, messenger := buildNotifier(start.Add(2*time.Hour), repo, &fakeCatalog{}, fakeBook{user: 3})
	if err := n.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(messenger.sent) != 0 {
		t.Errorf("a passed event must not remind: %+v", messenger.sent)
	}
}

func TestUnreachableUserIsSkipped(t *testing.T) {
	t.Parallel()

	user := uuid.New()
	repo := newFakeRepo(Settings{UserID: user, DigestAt: "09:00", Timezone: "UTC", Enabled: true})
	repo.events = []Event{{
		ID: uuid.New(), UserID: user, Title: "Что-то",
		StartsAt: time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC),
	}}

	n, messenger := buildNotifier(time.Date(2026, 9, 16, 9, 0, 0, 0, time.UTC), repo, &fakeCatalog{}, fakeBook{})

	if err := n.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(messenger.sent) != 0 {
		t.Errorf("nobody to write to, yet sent %d", len(messenger.sent))
	}
}

func TestCatalogueOutageDoesNotStopOwnEvents(t *testing.T) {
	t.Parallel()

	user := uuid.New()
	repo := newFakeRepo(Settings{UserID: user, DigestAt: "09:00", Timezone: "UTC", Enabled: true})
	repo.events = []Event{{
		ID: uuid.New(), UserID: user, Title: "Своё",
		StartsAt: time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC),
	}}
	catalog := &fakeCatalog{err: context.DeadlineExceeded}
	n, messenger := buildNotifier(time.Date(2026, 9, 16, 9, 0, 0, 0, time.UTC), repo, catalog, fakeBook{user: 11})

	if err := n.Run(context.Background()); err != nil {
		t.Fatalf("a catalogue outage must not fail the run: %v", err)
	}
	if len(messenger.sent) != 1 || !strings.Contains(messenger.sent[0].text, "Своё") {
		t.Errorf("own events must still go out: %+v", messenger.sent)
	}
}

func TestDigestIncludesReleasesAndSeasons(t *testing.T) {
	t.Parallel()

	user := uuid.New()
	season := 3
	repo := newFakeRepo(Settings{UserID: user, DigestAt: "09:00", Timezone: "UTC", Enabled: true})
	catalog := &fakeCatalog{events: []CatalogEvent{
		{UserID: user, Kind: KindFilmRelease, FilmID: 1, Title: "Дюна 3", Date: time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)},
		{UserID: user, Kind: KindSeasonStart, FilmID: 2, Title: "Разделение", SeasonNumber: &season, Date: time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)},
		{UserID: uuid.New(), Kind: KindFilmRelease, FilmID: 3, Title: "Чужое", Date: time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)},
	}}
	n, messenger := buildNotifier(time.Date(2026, 9, 16, 9, 0, 0, 0, time.UTC), repo, catalog, fakeBook{user: 42})

	if err := n.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(messenger.sent) != 1 {
		t.Fatalf("expected one digest, got %d", len(messenger.sent))
	}
	text := messenger.sent[0].text
	for _, want := range []string{"Дюна 3", "Разделение", "сезон 3"} {
		if !strings.Contains(text, want) {
			t.Errorf("digest missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Чужое") {
		t.Error("another user's release leaked into the digest")
	}
}

func TestRefsAreDistinctPerEntry(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	first, second := uint(10), uint(11)
	season := 2
	refs := map[string]bool{}
	for _, entry := range []Entry{
		{Kind: KindOwnEvent, EventID: &id},
		{Kind: KindFilmRelease, FilmID: &first},
		{Kind: KindFilmRelease, FilmID: &second},
		{Kind: KindSeasonStart, FilmID: &first, Season: &season},
	} {
		ref := entry.Ref()
		if refs[ref] {
			t.Errorf("duplicate ref %q", ref)
		}
		refs[ref] = true
	}
}

func TestNothingIsSentWhenNotificationsAreOff(t *testing.T) {
	t.Parallel()

	user := uuid.New()
	start := time.Date(2026, 9, 16, 15, 0, 0, 0, time.UTC)
	repo := newFakeRepo(Settings{UserID: user, DigestAt: "09:00", Timezone: "UTC", Enabled: false, LeadMinutes: 30})
	repo.events = []Event{{ID: uuid.New(), UserID: user, Title: "Врач", StartsAt: start}}
	catalog := &fakeCatalog{events: []CatalogEvent{
		{UserID: user, Kind: KindFilmRelease, FilmID: 1, Title: "Дюна 3", Date: time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)},
	}}

	n, messenger := buildNotifier(start.Add(-25*time.Minute), repo, catalog, fakeBook{user: 5})
	if err := n.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(messenger.sent) != 0 {
		t.Errorf("notifications are off, yet %d messages went out: %+v", len(messenger.sent), messenger.sent)
	}
}

func TestDigestCarriesEveryKind(t *testing.T) {
	t.Parallel()

	user := uuid.New()
	day := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	season := 2
	repo := newFakeRepo(Settings{UserID: user, DigestAt: "09:00", Timezone: "UTC", Enabled: true})
	repo.events = []Event{{
		ID: uuid.New(), UserID: user, Title: "Стоматолог", StartsAt: day.Add(14 * time.Hour),
	}}
	repo.imported = []ImportedEvent{{
		UserID: user, GoogleID: "g1", Title: "Планёрка", StartsAt: day.Add(10 * time.Hour),
	}}
	catalog := &fakeCatalog{events: []CatalogEvent{
		{UserID: user, Kind: KindFilmRelease, FilmID: 1, Title: "Дюна 3", Date: day},
		{UserID: user, Kind: KindSeasonStart, FilmID: 2, Title: "Разделение", SeasonNumber: &season, Date: day},
	}}

	n, messenger := buildNotifier(day.Add(9*time.Hour), repo, catalog, fakeBook{user: 8})
	if err := n.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(messenger.sent) != 1 {
		t.Fatalf("expected one digest, got %d", len(messenger.sent))
	}
	for _, want := range []string{"Стоматолог", "Планёрка", "Дюна 3", "Разделение"} {
		if !strings.Contains(messenger.sent[0].text, want) {
			t.Errorf("digest missing %q:\n%s", want, messenger.sent[0].text)
		}
	}
}

func TestOneLeadRemindsAboutEveryKind(t *testing.T) {
	t.Parallel()

	user := uuid.New()
	start := time.Date(2026, 9, 16, 15, 0, 0, 0, time.UTC)
	repo := newFakeRepo(Settings{UserID: user, DigestAt: "23:59", Timezone: "UTC", Enabled: true, LeadMinutes: 60})
	repo.events = []Event{{ID: uuid.New(), UserID: user, Title: "Врач", StartsAt: start}}
	repo.imported = []ImportedEvent{{UserID: user, GoogleID: "g1", Title: "Планёрка", StartsAt: start}}

	n, messenger := buildNotifier(start.Add(-30*time.Minute), repo, &fakeCatalog{}, fakeBook{user: 4})
	if err := n.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(messenger.sent) != 2 {
		t.Fatalf("expected a reminder for each entry, got %d: %+v", len(messenger.sent), messenger.sent)
	}
	joined := messenger.sent[0].text + messenger.sent[1].text
	for _, want := range []string{"Врач", "Планёрка"} {
		if !strings.Contains(joined, want) {
			t.Errorf("no reminder mentioned %q:\n%s", want, joined)
		}
	}
}

func TestReleaseReminderCountsBackFromItsUTCMidnight(t *testing.T) {
	t.Parallel()

	user := uuid.New()
	day := time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)
	repo := newFakeRepo(Settings{UserID: user, DigestAt: "23:59", Timezone: "UTC", Enabled: true, LeadMinutes: 60})
	catalog := &fakeCatalog{events: []CatalogEvent{
		{UserID: user, Kind: KindFilmRelease, FilmID: 1, Title: "Дюна 3", Date: day},
	}}

	n, messenger := buildNotifier(day.Add(-30*time.Minute), repo, catalog, fakeBook{user: 13})
	if err := n.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(messenger.sent) != 1 || !strings.Contains(messenger.sent[0].text, "Дюна 3") {
		t.Fatalf("expected one release reminder, got %+v", messenger.sent)
	}
}

func TestZeroLeadLeavesOnlyTheDigest(t *testing.T) {
	t.Parallel()

	user := uuid.New()
	start := time.Date(2026, 9, 16, 15, 0, 0, 0, time.UTC)
	repo := newFakeRepo(Settings{UserID: user, DigestAt: "23:59", Timezone: "UTC", Enabled: true, LeadMinutes: 0})
	repo.events = []Event{{ID: uuid.New(), UserID: user, Title: "Врач", StartsAt: start}}

	n, messenger := buildNotifier(start.Add(-5*time.Minute), repo, &fakeCatalog{}, fakeBook{user: 6})
	if err := n.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(messenger.sent) != 0 {
		t.Errorf("a zero lead must not remind: %+v", messenger.sent)
	}
}

func TestAllDayEntryStaysOnItsUTCDate(t *testing.T) {
	t.Parallel()

	user := uuid.New()
	repo := newFakeRepo(Settings{UserID: user, DigestAt: "09:00", Timezone: "Asia/Bangkok", Enabled: true})
	catalog := &fakeCatalog{events: []CatalogEvent{
		{UserID: user, Kind: KindFilmRelease, FilmID: 1, Title: "Дюна 3", Date: time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)},
	}}

	n, messenger := buildNotifier(time.Date(2026, 9, 16, 2, 0, 0, 0, time.UTC), repo, catalog, fakeBook{user: 12})
	if err := n.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(messenger.sent) != 1 || !strings.Contains(messenger.sent[0].text, "Дюна 3") {
		t.Errorf("an all-day release must land in its own day's digest: %+v", messenger.sent)
	}
}

func TestAllDayEntryKeepsItsUTCDateWestOfGreenwich(t *testing.T) {
	t.Parallel()

	user := uuid.New()
	repo := newFakeRepo(Settings{UserID: user, DigestAt: "09:00", Timezone: "America/New_York", Enabled: true})
	repo.events = []Event{{
		ID: uuid.New(), UserID: user, Title: "Отпуск", AllDay: true,
		StartsAt: time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC),
	}}

	n, messenger := buildNotifier(time.Date(2026, 9, 16, 13, 0, 0, 0, time.UTC), repo, &fakeCatalog{}, fakeBook{user: 5})
	if err := n.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(messenger.sent) != 1 || !strings.Contains(messenger.sent[0].text, "Отпуск") {
		t.Fatalf("an all-day entry must land in its own day's digest: %+v", messenger.sent)
	}
}

func TestLongLeadStillReachesFarAheadEntries(t *testing.T) {
	t.Parallel()

	user := uuid.New()

	repo := newFakeRepo(Settings{UserID: user, DigestAt: "23:59", Timezone: "UTC", Enabled: true, LeadMinutes: 30 * 24 * 60})
	now := time.Date(2026, 9, 16, 10, 0, 0, 0, time.UTC)
	repo.events = []Event{{ID: uuid.New(), UserID: user, Title: "Свадьба", StartsAt: now.AddDate(0, 0, 20)}}

	n, messenger := buildNotifier(now, repo, &fakeCatalog{}, fakeBook{user: 8})
	if err := n.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(messenger.sent) != 1 || !strings.Contains(messenger.sent[0].text, "Свадьба") {
		t.Fatalf("a 30-day lead must reach an entry 20 days out: %+v", messenger.sent)
	}
}
