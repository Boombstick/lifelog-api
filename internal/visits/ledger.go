package visits

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"sync"
	"time"
)

type Repo interface {
	Salt(ctx context.Context, day string) ([]byte, error)

	Touch(ctx context.Context, v VisitorDay) (int, error)
	AddRequests(ctx context.Context, requests []StoredRequest) error

	TrimRequests(ctx context.Context, day, visitor string, keep int) error
	Prune(ctx context.Context, saltsBefore, daysBefore string) error

	Days(ctx context.Context, from string) ([]DayCount, error)
	Visitors(ctx context.Context, day string) ([]VisitorDay, error)
	Requests(ctx context.Context, day, visitor string) ([]StoredRequest, error)
}

type VisitorDay struct {
	Day       string
	Visitor   string
	FirstSeen time.Time
	LastSeen  time.Time
	Requests  int
	UserAgent string
}

type StoredRequest struct {
	Day        string
	Visitor    string
	At         time.Time
	Service    string
	Method     string
	Path       string
	Status     int
	DurationMS float32
}

type DayCount struct {
	Day      string
	Visitors int
	Requests int
}

const (
	KeepRequests = 100

	KeepDays  = 30
	dayLayout = "2006-01-02"
)

type Ledger struct {
	repo Repo

	mu    sync.Mutex
	salts map[string][]byte
}

func NewLedger(repo Repo) *Ledger {
	return &Ledger{repo: repo, salts: map[string][]byte{}}
}

func (l *Ledger) salt(ctx context.Context, day string) ([]byte, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if salt, ok := l.salts[day]; ok {
		return salt, nil
	}
	salt, err := l.repo.Salt(ctx, day)
	if err != nil {
		return nil, err
	}
	for known := range l.salts {
		if known < day {
			delete(l.salts, known)
		}
	}
	l.salts[day] = salt
	return salt, nil
}

func VisitorID(salt []byte, ip string) string {
	mac := hmac.New(sha256.New, salt)
	mac.Write([]byte(ip))
	return hex.EncodeToString(mac.Sum(nil))[:16]
}

type visitKey struct{ day, visitor string }

func (l *Ledger) Record(ctx context.Context, events []Event) error {
	groups := map[visitKey]*VisitorDay{}
	var requests []StoredRequest
	var order []visitKey

	oldest := time.Now().UTC().Add(-time.Hour).Format(dayLayout)
	for _, e := range events {
		day := e.At.UTC().Format(dayLayout)
		if day < oldest {
			continue
		}
		salt, err := l.salt(ctx, day)
		if err != nil {
			return err
		}
		key := visitKey{day, VisitorID(salt, e.IP)}
		group, ok := groups[key]
		if !ok {
			group = &VisitorDay{Day: day, Visitor: key.visitor, FirstSeen: e.At, LastSeen: e.At}
			groups[key] = group
			order = append(order, key)
		}
		group.Requests++
		if e.At.Before(group.FirstSeen) {
			group.FirstSeen = e.At
		}
		if !e.At.Before(group.LastSeen) {
			group.LastSeen = e.At
			if e.UserAgent != "" {
				group.UserAgent = e.UserAgent
			}
		}
		requests = append(requests, StoredRequest{
			Day: day, Visitor: key.visitor, At: e.At, Service: e.Service,
			Method: e.Method, Path: e.Path, Status: e.Status, DurationMS: e.DurationMS,
		})
	}
	if err := l.repo.AddRequests(ctx, requests); err != nil {
		return err
	}
	for _, key := range order {
		total, err := l.repo.Touch(ctx, *groups[key])
		if err != nil {
			return err
		}
		if total > KeepRequests {
			if err := l.repo.TrimRequests(ctx, key.day, key.visitor, KeepRequests); err != nil {
				return err
			}
		}
	}
	return nil
}

func (l *Ledger) Prune(ctx context.Context, now time.Time) error {
	now = now.UTC()
	saltsBefore := now.Add(-time.Hour).Format(dayLayout)
	l.mu.Lock()
	for day := range l.salts {
		if day < saltsBefore {
			delete(l.salts, day)
		}
	}
	l.mu.Unlock()
	daysBefore := now.AddDate(0, 0, -KeepDays).Format(dayLayout)
	return l.repo.Prune(ctx, saltsBefore, daysBefore)
}

type Summary struct {
	Days []DayCount
	Max  int
}

func (l *Ledger) Summary(ctx context.Context, now time.Time) (Summary, error) {
	from := now.UTC().AddDate(0, 0, -KeepDays+1).Format(dayLayout)
	days, err := l.repo.Days(ctx, from)
	if err != nil {
		return Summary{}, err
	}
	byDay := map[string]DayCount{}
	for _, d := range days {
		byDay[d.Day] = d
	}
	summary := Summary{}
	for i := KeepDays - 1; i >= 0; i-- {
		day := now.UTC().AddDate(0, 0, -i).Format(dayLayout)
		count := byDay[day]
		count.Day = day
		summary.Days = append(summary.Days, count)
		summary.Max = max(summary.Max, count.Visitors)
	}
	return summary, nil
}

func (l *Ledger) Visitors(ctx context.Context, day string) ([]VisitorDay, error) {
	visitors, err := l.repo.Visitors(ctx, day)
	sort.SliceStable(visitors, func(i, j int) bool { return visitors[i].LastSeen.After(visitors[j].LastSeen) })
	return visitors, err
}

func (l *Ledger) Requests(ctx context.Context, day, visitor string) ([]StoredRequest, error) {
	return l.repo.Requests(ctx, day, visitor)
}

func ValidDay(s string) bool {
	_, err := time.Parse(dayLayout, s)
	return err == nil
}
