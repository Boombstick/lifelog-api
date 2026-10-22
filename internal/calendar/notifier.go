package calendar

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"lifelog-api/internal/notify"
)

type NotifierRepository interface {
	AllSettings(ctx context.Context) ([]Settings, error)
	AllEventsBetween(ctx context.Context, from, to time.Time) ([]Event, error)
	AllImportedBetween(ctx context.Context, from, to time.Time) ([]ImportedEvent, error)
	MarkSent(ctx context.Context, userID uuid.UUID, ref, slot string) (bool, error)

	ForgetDelivery(ctx context.Context, userID uuid.UUID, ref, slot string) error
	ForgetDeliveries(ctx context.Context, before time.Time) (int64, error)
}

type Addressbook interface {
	TelegramIDs(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID]int64, error)
}

type Messenger interface {
	Send(ctx context.Context, chatID int64, text string) error
}

type Pusher interface {
	Push(ctx context.Context, message PushMessage, userIDs []uuid.UUID) error
}

type PushMessage struct {
	Kind        string
	Title       string
	Body        string
	URL         string
	CollapseKey string
	TTL         time.Duration
}

const pushURL = "/calendar"

const digestGrace = 3 * time.Hour

type Notifier struct {
	repo      NotifierRepository
	catalog   CatalogSource
	addresses Addressbook
	messenger Messenger

	pusher Pusher
	logger *slog.Logger

	now func() time.Time
}

func NewNotifier(
	repo NotifierRepository,
	catalog CatalogSource,
	addresses Addressbook,
	messenger Messenger,
	pusher Pusher,
	logger *slog.Logger,
) *Notifier {
	return &Notifier{
		repo:      repo,
		catalog:   catalog,
		addresses: addresses,
		messenger: messenger,
		pusher:    pusher,
		logger:    logger,
		now:       func() time.Time { return time.Now().UTC() },
	}
}

func (n *Notifier) Run(ctx context.Context) error {
	now := n.now()

	settings, err := n.repo.AllSettings(ctx)
	if err != nil {
		return fmt.Errorf("settings: %w", err)
	}
	if len(settings) == 0 {
		return nil
	}

	enabled := make([]Settings, 0, len(settings))
	for _, s := range settings {
		if s.Enabled {
			enabled = append(enabled, s)
		}
	}
	settings = enabled
	if len(settings) == 0 {
		return nil
	}

	userIDs := make([]uuid.UUID, 0, len(settings))
	for _, s := range settings {
		userIDs = append(userIDs, s.UserID)
	}
	chats, err := n.addresses.TelegramIDs(ctx, userIDs)
	if err != nil {
		return fmt.Errorf("telegram links: %w", err)
	}
	if len(chats) == 0 && n.pusher == nil {
		return nil
	}

	from := now
	for _, setting := range settings {
		if start := todayStart(now, loadZone(setting.Timezone)); start.Before(from) {
			from = start
		}
	}
	horizon := maxLead(settings)
	entriesByUser, err := n.collect(ctx, from, now.Add(horizon+48*time.Hour))
	if err != nil {
		return err
	}

	var due []outgoing
	for _, setting := range settings {
		if _, hasChat := chats[setting.UserID]; !hasChat && n.pusher == nil {
			continue
		}
		entries := entriesByUser[setting.UserID]

		digest, err := n.digest(setting, entries, now)
		if err != nil {
			n.logger.Error("digest failed", "user", setting.UserID, "error", err)
		} else if digest != nil {
			due = append(due, *digest)
		}
		due = append(due, n.reminders(setting, entries, now)...)
	}
	n.deliver(ctx, due, chats)

	if _, err := n.repo.ForgetDeliveries(ctx, now.AddDate(0, 0, -30)); err != nil {
		n.logger.Warn("could not trim delivery records", "error", err)
	}
	return nil
}

func (n *Notifier) collect(ctx context.Context, from, to time.Time) (map[uuid.UUID][]Entry, error) {
	out := make(map[uuid.UUID][]Entry)

	events, err := n.repo.AllEventsBetween(ctx, from, to)
	if err != nil {
		return nil, fmt.Errorf("events: %w", err)
	}
	for _, event := range events {
		id := event.ID

		out[event.UserID] = append(out[event.UserID], Entry{
			Kind:     KindOwnEvent,
			When:     wireTime(event.StartsAt, event.AllDay),
			EndsAt:   wireEnd(event.EndsAt, event.AllDay),
			AllDay:   event.AllDay,
			Title:    event.Title,
			Details:  event.Details,
			Location: event.Location,
			EventID:  &id,
		})
	}

	imported, err := n.repo.AllImportedBetween(ctx, from, to)
	if err != nil {
		return nil, fmt.Errorf("imported events: %w", err)
	}
	for _, event := range imported {
		out[event.UserID] = append(out[event.UserID], entryFromImported(event))
	}

	if n.catalog != nil {
		catalogEvents, err := n.catalog.Upcoming(ctx, from, to, uuid.Nil)
		if err != nil {
			n.logger.Warn("catalogue unavailable this run", "error", err)
		} else {
			for _, item := range catalogEvents {
				out[item.UserID] = append(out[item.UserID], entryFromCatalog(item))
			}
		}
	}
	return out, nil
}

type outgoing struct {
	userID uuid.UUID
	ref    string
	slot   string
	text   string
	push   PushMessage
}

func (n *Notifier) digest(setting Settings, entries []Entry, now time.Time) (*outgoing, error) {
	location := loadZone(setting.Timezone)
	hour, minute, err := ParseDigestAt(setting.DigestAt)
	if err != nil {
		return nil, err
	}

	local := now.In(location)

	if local.Hour() < hour || (local.Hour() == hour && local.Minute() < minute) {
		return nil, nil
	}

	year, month, day := local.Date()
	if now.Sub(time.Date(year, month, day, hour, minute, 0, 0, location)) >= digestGrace {
		return nil, nil
	}

	today := local.Format("2006-01-02")
	var due []Entry
	for _, entry := range entries {
		when := entry.When.In(location)
		if entry.AllDay {
			when = entry.When.UTC()
		}
		if when.Format("2006-01-02") == today {
			due = append(due, entry)
		}
	}

	if len(due) == 0 {
		return nil, nil
	}

	lines := make([]string, 0, len(due))
	for _, entry := range due {
		lines = append(lines, "• "+entryLine(entry, location))
	}
	return &outgoing{
		userID: setting.UserID,
		ref:    "digest",
		slot:   "digest:" + today,
		text:   digestText(due, location),
		push: PushMessage{
			Kind:        notify.KindDigest,
			Title:       "Сегодня",
			Body:        strings.Join(lines, "\n"),
			URL:         pushURL,
			CollapseKey: "digest",
			TTL:         12 * time.Hour,
		},
	}, nil
}

func (n *Notifier) reminders(setting Settings, entries []Entry, now time.Time) []outgoing {
	lead := setting.LeadMinutes
	if lead <= 0 {
		return nil
	}
	location := loadZone(setting.Timezone)

	var out []outgoing
	for _, entry := range entries {
		start := entryStart(entry, location)
		fireAt := start.Add(-time.Duration(lead) * time.Minute)

		if now.Before(fireAt) || !now.Before(start) {
			continue
		}

		ttl := max(start.Sub(now).Truncate(time.Second), time.Minute)
		out = append(out, outgoing{
			userID: setting.UserID,
			ref:    entry.Ref(),

			slot: "lead:" + entry.When.UTC().Format(time.RFC3339),
			text: reminderText(entry, location, lead, now),
			push: PushMessage{
				Kind:        notify.KindReminder,
				Title:       entryLine(entry, location),
				Body:        reminderLead(entry, location, lead, now),
				URL:         pushURL,
				CollapseKey: "reminder:" + entry.Ref(),
				TTL:         ttl,
			},
		})
	}
	return out
}

func (n *Notifier) deliver(ctx context.Context, due []outgoing, chats map[uuid.UUID]int64) {
	type claimed struct {
		outgoing
		telegram bool
	}
	var taken []claimed
	pushes := map[PushMessage][]uuid.UUID{}
	var order []PushMessage

	for _, message := range due {
		fresh, err := n.repo.MarkSent(ctx, message.userID, message.ref, message.slot)
		if err != nil {
			n.logger.Error("delivery not recorded", "user", message.userID, "error", err)
			continue
		}
		if !fresh {
			continue
		}
		c := claimed{outgoing: message}
		if chatID, ok := chats[message.userID]; ok {
			if err := n.messenger.Send(ctx, chatID, message.text); err != nil {
				n.logger.Warn("telegram send failed", "user", message.userID, "error", err)
			} else {
				c.telegram = true
			}
		}
		if n.pusher != nil {
			if _, seen := pushes[message.push]; !seen {
				order = append(order, message.push)
			}
			pushes[message.push] = append(pushes[message.push], message.userID)
		}
		taken = append(taken, c)
	}

	pushed := make(map[PushMessage]bool, len(pushes))
	for _, message := range order {
		if err := n.pusher.Push(ctx, message, pushes[message]); err != nil {
			n.logger.Warn("push not accepted", "kind", message.Kind, "users", len(pushes[message]), "error", err)
			continue
		}
		pushed[message] = true
	}

	for _, c := range taken {
		if c.telegram || pushed[c.push] {
			continue
		}
		if err := n.repo.ForgetDelivery(ctx, c.userID, c.ref, c.slot); err != nil {
			n.logger.Error("failed delivery not released, it will not be retried",
				"user", c.userID, "error", err)
		}
	}
}

func maxLead(settings []Settings) time.Duration {
	longest := 30 * time.Minute
	for _, setting := range settings {
		if lead := time.Duration(setting.LeadMinutes) * time.Minute; lead > longest {
			longest = lead
		}
	}
	return longest
}

func digestText(entries []Entry, location *time.Location) string {
	var b strings.Builder
	b.WriteString("Сегодня:\n")
	for _, entry := range entries {
		b.WriteString("\n• ")
		b.WriteString(entryLine(entry, location))
	}
	return b.String()
}

func reminderText(entry Entry, location *time.Location, leadMinutes int, now time.Time) string {
	return reminderLead(entry, location, leadMinutes, now) + ":\n\n" + entryLine(entry, location)
}

func reminderLead(entry Entry, location *time.Location, leadMinutes int, now time.Time) string {
	if entry.AllDay {
		return allDayLead(entry, location, now)
	}
	when := entry.When.In(location).Format("15:04")
	lead := time.Duration(leadMinutes) * time.Minute
	return fmt.Sprintf("Через %s, в %s", humanLead(lead), when)
}

func allDayLead(entry Entry, location *time.Location, now time.Time) string {
	year, month, day := now.In(location).Date()
	today := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	days := int(dateAsWritten(entry.When.UTC()).Sub(today).Hours() / 24)
	switch {
	case days <= 0:
		return "Сегодня весь день"
	case days == 1:
		return "Завтра весь день"
	case days == 2:
		return "Послезавтра весь день"
	default:
		return fmt.Sprintf("Через %d дн., весь день", days)
	}
}

func entryStart(entry Entry, location *time.Location) time.Time {
	if !entry.AllDay {
		return entry.When
	}
	year, month, day := entry.When.UTC().Date()
	return time.Date(year, month, day, 0, 0, 0, 0, location)
}

func todayStart(now time.Time, location *time.Location) time.Time {
	year, month, day := now.In(location).Date()
	local := time.Date(year, month, day, 0, 0, 0, 0, location)
	if utc := time.Date(year, month, day, 0, 0, 0, 0, time.UTC); utc.Before(local) {
		return utc
	}
	return local
}

func loadZone(name string) *time.Location {
	location, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return location
}

func entryLine(entry Entry, location *time.Location) string {
	var b strings.Builder
	switch entry.Kind {
	case KindFilmRelease:
		b.WriteString("Премьера: ")
	case KindSeasonStart:
		b.WriteString("Новый сезон: ")
	}
	b.WriteString(entry.Title)
	if entry.Kind == KindSeasonStart && entry.Season != nil {
		fmt.Fprintf(&b, ", сезон %d", *entry.Season)
	}
	if (entry.Kind == KindOwnEvent || entry.Kind == KindGoogleEvent) && !entry.AllDay {
		fmt.Fprintf(&b, " — %s", entry.When.In(location).Format("15:04"))
	}
	if entry.Location != "" {
		fmt.Fprintf(&b, " (%s)", entry.Location)
	}
	return b.String()
}

func humanLead(lead time.Duration) string {
	switch {
	case lead >= 48*time.Hour:
		return fmt.Sprintf("%d дн.", int(lead.Hours()/24))
	case lead >= 24*time.Hour:
		return "сутки"
	case lead >= time.Hour:
		return fmt.Sprintf("%d ч.", int(lead.Hours()))
	default:
		return fmt.Sprintf("%d мин.", int(lead.Minutes()))
	}
}
