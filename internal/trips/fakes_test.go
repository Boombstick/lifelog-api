package trips

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type memStore struct {
	mu sync.Mutex

	users      map[uuid.UUID]User
	countries  []Country
	currencies []Currency
	categories map[int64]Category
	trips      map[uuid.UUID]Trip
	expenses   map[int64]Expense
	photos     map[uuid.UUID]Photo
	clientIDs  map[[2]uuid.UUID]uuid.UUID
	syncs      map[[2]uuid.UUID]json.RawMessage

	nextExpenseID  int64
	nextCategoryID int64
	allowTwoActive bool

	calls map[string]int
	log   []string

	failInsertTrip     error
	failInsertCategory error
	failSetKeywords    error
	failSeed           error
	failDeletePhoto    error
	failDeleteTrip     error
	seeded             *SeedData
}

func newMemStore() *memStore {
	return &memStore{
		users:          map[uuid.UUID]User{},
		categories:     map[int64]Category{},
		trips:          map[uuid.UUID]Trip{},
		expenses:       map[int64]Expense{},
		photos:         map[uuid.UUID]Photo{},
		clientIDs:      map[[2]uuid.UUID]uuid.UUID{},
		syncs:          map[[2]uuid.UUID]json.RawMessage{},
		nextExpenseID:  1,
		nextCategoryID: 11,
		calls:          map[string]int{},
	}
}

func (s *memStore) called(name string) {
	s.calls[name]++
	s.log = append(s.log, name)
}

var _ Store = (*memStore)(nil)

func (s *memStore) InTx(ctx context.Context, fn func(tx Store) error) error { return fn(s) }

func (s *memStore) UserByAuthID(ctx context.Context, authUserID uuid.UUID) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.users {
		if u.AuthUserID != nil && *u.AuthUserID == authUserID {
			return &u, nil
		}
	}
	return nil, nil
}

func (s *memStore) UserByTelegramID(ctx context.Context, telegramID int64) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.users {
		if u.TelegramID != nil && *u.TelegramID == telegramID {
			return &u, nil
		}
	}
	return nil, nil
}

func (s *memStore) InsertUser(ctx context.Context, user *User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users[user.ID] = *user
	return nil
}

func (s *memStore) SetUserAuthID(ctx context.Context, userID, authUserID uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.users[userID]
	u.AuthUserID = &authUserID
	s.users[userID] = u
	return nil
}

func (s *memStore) SetUserTelegramID(ctx context.Context, userID uuid.UUID, telegramID *int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.users[userID]
	u.TelegramID = telegramID
	s.users[userID] = u
	return nil
}

func (s *memStore) DeleteUser(ctx context.Context, userID uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.users, userID)
	return nil
}

func (s *memStore) Countries(ctx context.Context) ([]Country, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.called("Countries")
	return append([]Country(nil), s.countries...), nil
}

func (s *memStore) Currencies(ctx context.Context) ([]Currency, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.called("Currencies")
	return append([]Currency(nil), s.currencies...), nil
}

func (s *memStore) CountryByName(ctx context.Context, name string) (*Country, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.called("CountryByName:" + name)
	var found []Country
	for _, c := range s.countries {
		if strings.ToLower(c.Name) == name || strings.ToLower(c.NameRu) == name {
			found = append(found, c)
		}
	}
	switch len(found) {
	case 0:
		return nil, nil
	case 1:
		return &found[0], nil
	}
	return nil, Conflict(MsgSequenceMoreThanOne)
}

func (s *memStore) CurrencyByCode(ctx context.Context, code string) (*Currency, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.called("CurrencyByCode:" + code)
	for _, c := range s.currencies {
		if c.Code == code {
			return &c, nil
		}
	}
	return nil, nil
}

func (s *memStore) Categories(ctx context.Context) ([]Category, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.called("Categories")
	ids := make([]int64, 0, len(s.categories))
	for id := range s.categories {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	result := make([]Category, 0, len(ids))
	for _, id := range ids {
		result = append(result, copyCategory(s.categories[id]))
	}
	return result, nil
}

func (s *memStore) CategoryByID(ctx context.Context, id int64) (*Category, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.called("CategoryByID")
	c, ok := s.categories[id]
	if !ok {
		return nil, nil
	}
	c = copyCategory(c)
	return &c, nil
}

func (s *memStore) InsertCategory(ctx context.Context, category *Category) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.called("InsertCategory")
	if s.failInsertCategory != nil {
		return s.failInsertCategory
	}
	category.ID = s.nextCategoryID
	s.nextCategoryID++
	s.categories[category.ID] = copyCategory(*category)
	return nil
}

func (s *memStore) SetCategoryKeywords(ctx context.Context, id int64, keywords []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.called("SetCategoryKeywords")
	if s.failSetKeywords != nil {
		return s.failSetKeywords
	}
	c := s.categories[id]
	c.Keywords = append([]string(nil), keywords...)
	s.categories[id] = c
	return nil
}

func (s *memStore) SeedDictionaries(ctx context.Context, data SeedData) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.called("SeedDictionaries")
	if s.failSeed != nil {
		return s.failSeed
	}
	s.seeded = &data
	s.currencies = append([]Currency(nil), data.Currencies...)
	s.countries = append([]Country(nil), data.Countries...)
	for _, c := range data.Categories {
		if existing, ok := s.categories[c.ID]; ok {
			existing.Name = c.Name
			s.categories[c.ID] = existing
			continue
		}
		s.categories[c.ID] = copyCategory(c)
	}
	return nil
}

func copyCategory(c Category) Category {
	if c.Keywords != nil {
		c.Keywords = append([]string{}, c.Keywords...)
	}
	return c
}

func (s *memStore) withCountryName(t Trip) Trip {
	t.CountryName = nil
	if t.CountryCode != nil {
		for _, c := range s.countries {
			if c.Code == *t.CountryCode {
				name := c.NameRu
				t.CountryName = &name
			}
		}
	}
	return t
}

func (s *memStore) sortedTrips(filter func(Trip) bool) []Trip {
	var result []Trip
	for _, t := range s.trips {
		if filter(t) {
			result = append(result, s.withCountryName(t))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID.String() < result[j].ID.String() })
	return result
}

func (s *memStore) ActiveTrips(ctx context.Context, userID uuid.UUID) ([]Trip, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	trips := s.sortedTrips(func(t Trip) bool { return t.UserID == userID && t.Status == StatusActive })
	if len(trips) > 2 {
		trips = trips[:2]
	}
	return trips, nil
}

func (s *memStore) OwnedTrip(ctx context.Context, userID, tripID uuid.UUID) (*Trip, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.trips[tripID]
	if !ok || t.UserID != userID {
		return nil, nil
	}
	t = s.withCountryName(t)
	return &t, nil
}

func (s *memStore) TripsOfUser(ctx context.Context, userID uuid.UUID) ([]Trip, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sortedTrips(func(t Trip) bool { return t.UserID == userID }), nil
}

func (s *memStore) TripHistory(ctx context.Context, userID uuid.UUID) ([]TripSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []TripSummary
	for _, t := range s.sortedTrips(func(t Trip) bool { return t.UserID == userID }) {
		summary := TripSummary{Trip: t}
		for _, e := range s.expenses {
			if e.TripID == t.ID && e.DuplicateOfID == nil {
				summary.TotalUSD = DecimalFrom(summary.TotalUSD.Add(e.AmountUSD.Decimal))
				summary.ExpenseCount++
			}
		}
		result = append(result, summary)
	}
	return result, nil
}

func (s *memStore) checkActive(trip *Trip) error {
	if s.allowTwoActive || trip.Status != StatusActive {
		return nil
	}
	for _, t := range s.trips {
		if t.ID != trip.ID && t.UserID == trip.UserID && t.Status == StatusActive {
			return ErrActiveTripExists
		}
	}
	return nil
}

func (s *memStore) InsertTrip(ctx context.Context, trip *Trip) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.called("InsertTrip")
	if s.failInsertTrip != nil {
		return s.failInsertTrip
	}
	if err := s.checkActive(trip); err != nil {
		return err
	}
	stored := *trip
	stored.CountryName = nil
	s.trips[trip.ID] = stored
	return nil
}

func (s *memStore) UpdateTrip(ctx context.Context, trip *Trip) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.called("UpdateTrip")
	if err := s.checkActive(trip); err != nil {
		return err
	}
	stored := *trip
	stored.CountryName = nil
	s.trips[trip.ID] = stored
	return nil
}

func (s *memStore) DeleteTrip(ctx context.Context, userID, tripID uuid.UUID) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.called("DeleteTrip")
	if s.failDeleteTrip != nil {
		return false, s.failDeleteTrip
	}
	t, ok := s.trips[tripID]
	if !ok || t.UserID != userID {
		return false, nil
	}
	for _, e := range s.expenses {
		if e.TripID == tripID {
			return false, &IntegrityError{
				SQLState:   "23503",
				Constraint: "fk_expenses_trips_trip_id",
				Message:    "update or delete on table \"trips\" violates foreign key constraint",
			}
		}
	}
	delete(s.trips, tripID)

	for key, id := range s.clientIDs {
		if id == tripID {
			delete(s.clientIDs, key)
		}
	}
	for id, p := range s.photos {
		if p.TripID == tripID {
			delete(s.photos, id)
		}
	}
	return true, nil
}

func (s *memStore) TripIDByClientID(ctx context.Context, userID, clientID uuid.UUID) (*uuid.UUID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.clientIDs[[2]uuid.UUID{userID, clientID}]
	if !ok {
		return nil, nil
	}
	return &id, nil
}

func (s *memStore) InsertTripClientID(ctx context.Context, userID, clientID, tripID uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := [2]uuid.UUID{userID, clientID}
	if _, ok := s.clientIDs[key]; ok {
		return &IntegrityError{SQLState: "23505", Message: "duplicate key"}
	}
	s.clientIDs[key] = tripID
	return nil
}

func (s *memStore) TripClientIDs(ctx context.Context, tripID uuid.UUID) ([]uuid.UUID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := []uuid.UUID{}
	for key, id := range s.clientIDs {
		if id == tripID {
			result = append(result, key[1])
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].String() < result[j].String() })
	return result, nil
}

func (s *memStore) detail(e Expense) ExpenseDetail {
	d := ExpenseDetail{Expense: e}
	d.CategoryName = s.categories[e.CategoryID].Name
	if t, ok := s.trips[e.TripID]; ok {
		d.TripUserID = t.UserID
		d.TripName = t.Name
		d.TripCity = t.City
	}
	return d
}

func (s *memStore) sortedExpenses(filter func(ExpenseDetail) bool) []ExpenseDetail {
	var result []ExpenseDetail
	for _, e := range s.expenses {
		d := s.detail(e)
		if filter(d) {
			result = append(result, d)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func inRange(d Date, r DateRange) bool {
	return (r.From == nil || !d.Before(*r.From)) && (r.To == nil || !d.After(*r.To))
}

func (s *memStore) ExpenseByClientID(ctx context.Context, clientID uuid.UUID) (*ExpenseDetail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.expenses {
		if e.ClientID != nil && *e.ClientID == clientID {
			d := s.detail(e)
			return &d, nil
		}
	}
	return nil, nil
}

func (s *memStore) OwnedExpense(ctx context.Context, userID uuid.UUID, id int64) (*ExpenseDetail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.expenses[id]
	if !ok {
		return nil, nil
	}
	d := s.detail(e)
	if d.TripUserID != userID {
		return nil, nil
	}
	return &d, nil
}

func (s *memStore) LookAlike(ctx context.Context, q LookAlikeQuery) (*int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.sortedExpenses(func(d ExpenseDetail) bool {
		return d.TripID == q.TripID && d.Amount.Equal(q.Amount.Decimal) && d.CurrencyCode == q.CurrencyCode &&
			d.OccurredAt == q.OccurredAt && d.Source != q.Source && d.DuplicateOfID == nil
	}) {
		id := d.ID
		return &id, nil
	}
	return nil, nil
}

func (s *memStore) InsertExpense(ctx context.Context, expense *Expense) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.called("InsertExpense")
	expense.ID = s.nextExpenseID
	s.nextExpenseID++
	stored := *expense

	stored.Amount = DecimalFrom(stored.Amount.Round(2))
	s.expenses[expense.ID] = stored
	return nil
}

func (s *memStore) ListExpenses(ctx context.Context, filter ExpenseFilter) ([]ExpenseDetail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := s.sortedExpenses(func(d ExpenseDetail) bool {
		if d.TripUserID != filter.UserID {
			return false
		}
		if filter.TripID != nil {
			if d.TripID != *filter.TripID {
				return false
			}
		} else if s.trips[d.TripID].Status != StatusActive {
			return false
		}
		return filter.CategoryID == nil || d.CategoryID == *filter.CategoryID
	})
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].OccurredAt != rows[j].OccurredAt {
			return rows[i].OccurredAt.After(rows[j].OccurredAt)
		}
		return rows[i].ID > rows[j].ID
	})
	return rows, nil
}

func (s *memStore) TotalAmount(ctx context.Context, userID uuid.UUID, r DateRange) (Decimal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	total := Decimal{}
	for _, d := range s.sortedExpenses(func(d ExpenseDetail) bool {
		return d.TripUserID == userID && d.DuplicateOfID == nil && s.trips[d.TripID].Status == StatusActive && inRange(d.OccurredAt, r)
	}) {
		total = DecimalFrom(total.Add(d.Amount.Decimal))
	}
	return total, nil
}

func (s *memStore) ReportRows(ctx context.Context, userID uuid.UUID, r DateRange) ([]ReportRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var rows []ReportRow
	for _, d := range s.sortedExpenses(func(d ExpenseDetail) bool {
		return d.TripUserID == userID && d.DuplicateOfID == nil && inRange(d.OccurredAt, r)
	}) {
		rows = append(rows, ReportRow{CategoryName: d.CategoryName, Amount: d.Amount, CurrencyCode: d.CurrencyCode})
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].CurrencyCode < rows[j].CurrencyCode })
	return rows, nil
}

func (s *memStore) BuilderRows(ctx context.Context, userID uuid.UUID, r DateRange) ([]ExpenseDetail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.called("BuilderRows")
	return s.sortedExpenses(func(d ExpenseDetail) bool {
		return d.TripUserID == userID && d.DuplicateOfID == nil && inRange(d.OccurredAt, r)
	}), nil
}

func (s *memStore) FlaggedExpenses(ctx context.Context, userID uuid.UUID) ([]ExpenseDetail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := s.sortedExpenses(func(d ExpenseDetail) bool { return d.TripUserID == userID && d.DuplicateOfID != nil })
	sort.SliceStable(rows, func(i, j int) bool {
		if !rows[i].CreatedAt.Equal(rows[j].CreatedAt) {
			return rows[i].CreatedAt.After(rows[j].CreatedAt)
		}
		return rows[i].ID > rows[j].ID
	})
	return rows, nil
}

func (s *memStore) ClearDuplicateFlag(ctx context.Context, userID uuid.UUID, id int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.expenses[id]
	if !ok || s.trips[e.TripID].UserID != userID {
		return false, nil
	}
	e.DuplicateOfID = nil
	s.expenses[id] = e
	return true, nil
}

func (s *memStore) DeleteExpense(ctx context.Context, userID uuid.UUID, id int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.expenses[id]
	if !ok || s.trips[e.TripID].UserID != userID {
		return false, nil
	}
	delete(s.expenses, id)
	for otherID, other := range s.expenses {
		if other.DuplicateOfID != nil && *other.DuplicateOfID == id {
			other.DuplicateOfID = nil
			s.expenses[otherID] = other
		}
	}
	return true, nil
}

func (s *memStore) DeleteExpensesOfTrip(ctx context.Context, tripID uuid.UUID) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.called("DeleteExpensesOfTrip")
	deleted := 0
	for id, e := range s.expenses {
		if e.TripID == tripID {
			delete(s.expenses, id)
			deleted++
		}
	}
	for id, e := range s.expenses {
		if e.DuplicateOfID != nil {
			if _, alive := s.expenses[*e.DuplicateOfID]; !alive {
				e.DuplicateOfID = nil
				s.expenses[id] = e
			}
		}
	}
	return deleted, nil
}

func (s *memStore) TripPhotos(ctx context.Context, userID, tripID uuid.UUID) ([]Photo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []Photo
	if s.trips[tripID].UserID != userID {
		return result, nil
	}
	for _, p := range s.photos {
		if p.TripID == tripID {
			result = append(result, p)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.After(result[j].CreatedAt) })
	return result, nil
}

func (s *memStore) OwnedPhoto(ctx context.Context, userID, photoID uuid.UUID) (*Photo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.photos[photoID]
	if !ok || s.trips[p.TripID].UserID != userID {
		return nil, nil
	}
	return &p, nil
}

func (s *memStore) InsertPhoto(ctx context.Context, photo *Photo) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.called("InsertPhoto")
	s.photos[photo.ID] = *photo
	return nil
}

func (s *memStore) DeletePhoto(ctx context.Context, photoID uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.called("DeletePhoto")
	if s.failDeletePhoto != nil {
		return s.failDeletePhoto
	}
	delete(s.photos, photoID)
	return nil
}

func (s *memStore) DeleteTripPhotos(ctx context.Context, tripID uuid.UUID) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.called("DeleteTripPhotos")
	deleted := 0
	for id, p := range s.photos {
		if p.TripID == tripID {
			delete(s.photos, id)
			deleted++
		}
	}
	return deleted, nil
}

func (s *memStore) SyncResult(ctx context.Context, userID, opID uuid.UUID) (json.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.syncs[[2]uuid.UUID{userID, opID}], nil
}

func (s *memStore) SaveSyncResult(ctx context.Context, userID, opID uuid.UUID, result json.RawMessage, processedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.syncs[[2]uuid.UUID{userID, opID}] = result
	return nil
}

func (s *memStore) ReassignSyncData(ctx context.Context, fromUserID, toUserID uuid.UUID) error {
	return nil
}

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

type fakeSearch struct {
	hits map[string]CategoryRef
	err  error
}

func (f *fakeSearch) Search(ctx context.Context, description string) (*CategoryRef, error) {
	if f.err != nil {
		return nil, f.err
	}
	for word, ref := range f.hits {
		if strings.Contains(description, word) {
			r := ref
			return &r, nil
		}
	}
	return nil, nil
}

type fakeIndex struct {
	calls       []string
	ensureErr   error
	syncErr     error
	putErr      error
	keywordsErr error
	synced      []Category
	put         []Category
	added       map[int64][]string
}

func (f *fakeIndex) Ensure(ctx context.Context) error {
	f.calls = append(f.calls, "Ensure")
	return f.ensureErr
}

func (f *fakeIndex) SyncAll(ctx context.Context, categories []Category) error {
	f.calls = append(f.calls, "SyncAll")
	f.synced = categories
	return f.syncErr
}

func (f *fakeIndex) Put(ctx context.Context, category Category) error {
	f.calls = append(f.calls, "Put")
	f.put = append(f.put, category)
	return f.putErr
}

func (f *fakeIndex) AddKeywords(ctx context.Context, id int64, keywords []string) error {
	f.calls = append(f.calls, "AddKeywords")
	if f.added == nil {
		f.added = map[int64][]string{}
	}
	f.added[id] = keywords
	return f.keywordsErr
}

type fakeRates struct {
	snapshots []Snapshot
	err       error
}

func (f *fakeRates) Latest(ctx context.Context) (*Snapshot, error) {
	if f.err != nil {
		return nil, f.err
	}
	var latest *Snapshot
	for i := range f.snapshots {
		if latest == nil || f.snapshots[i].ID > latest.ID {
			latest = &f.snapshots[i]
		}
	}
	return latest, nil
}

func (f *fakeRates) Closest(ctx context.Context, at time.Time) (*Snapshot, error) {
	if f.err != nil {
		return nil, f.err
	}
	var before, after *Snapshot
	for i := range f.snapshots {
		sn := &f.snapshots[i]
		if !sn.CreatedAt.After(at) {
			if before == nil || sn.CreatedAt.After(before.CreatedAt) {
				before = sn
			}
		} else if after == nil || sn.CreatedAt.Before(after.CreatedAt) {
			after = sn
		}
	}
	switch {
	case before == nil:
		return after, nil
	case after == nil:
		return before, nil
	case at.Sub(before.CreatedAt) <= after.CreatedAt.Sub(at):
		return before, nil
	}
	return after, nil
}

type fakeFiles struct {
	files     map[string][]byte
	saveErr   error
	deleteErr error
	deleteLog []string
	log       *[]string
}

func newFakeFiles() *fakeFiles { return &fakeFiles{files: map[string][]byte{}} }

func (f *fakeFiles) Save(ctx context.Context, name string, body io.Reader) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	f.files[name] = data
	return nil
}

func (f *fakeFiles) Open(ctx context.Context, name string) (io.ReadCloser, error) {
	data, ok := f.files[name]
	if !ok {
		return nil, ErrPhotoFileMissing
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (f *fakeFiles) Delete(ctx context.Context, name string) error {
	f.deleteLog = append(f.deleteLog, name)
	if f.log != nil {
		*f.log = append(*f.log, "file:Delete")
	}
	if f.deleteErr != nil {
		return f.deleteErr
	}
	delete(f.files, name)
	return nil
}

type fakeTZ struct {
	zone      string
	offset    time.Duration
	lookupErr error
	offsetErr error
	gotYear   int
}

func (f *fakeTZ) Lookup(latitude, longitude float64) (string, error) {
	if f.lookupErr != nil {
		return "", f.lookupErr
	}
	return f.zone, nil
}

func (f *fakeTZ) BaseOffset(timezone string, year int) (time.Duration, error) {
	f.gotYear = year
	if f.offsetErr != nil {
		return 0, f.offsetErr
	}
	return f.offset, nil
}

var bg = context.Background()

var quietLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

var testNow = time.Date(2026, 9, 16, 12, 30, 0, 0, time.UTC)

func ptr[T any](v T) *T { return &v }

func appActor(userID uuid.UUID) Actor { return Actor{UserID: userID, Mode: ModeApp} }
func botActor(userID uuid.UUID) Actor { return Actor{UserID: userID, Mode: ModeBot} }

var (
	thailand = Country{Code: "TH", Name: "Thailand", NameRu: "Таиланд", DefaultCurrencyCode: "THB", DefaultTimezone: "Asia/Bangkok"}
	russia   = Country{Code: "RU", Name: "Russia", NameRu: "Россия", DefaultCurrencyCode: "RUB", DefaultTimezone: "Europe/Moscow", IsMultiTimezone: true}
	georgia  = Country{Code: "GE", Name: "Georgia", NameRu: "Грузия", DefaultCurrencyCode: "GEL", DefaultTimezone: "Asia/Tbilisi", IsMultiCurrency: true}
)

func seededStore() *memStore {
	s := newMemStore()
	s.countries = []Country{thailand, russia, georgia}
	s.currencies = []Currency{
		{Code: "GEL", Name: "Georgian Lari", NameRu: "Грузинский лари", Symbol: "₾"},
		{Code: "RUB", Name: "Russian Ruble", NameRu: "Российский рубль", Symbol: "₽"},
		{Code: "THB", Name: "Thai Baht", NameRu: "Тайский бат", Symbol: "฿"},
		{Code: "USD", Name: "US Dollar", NameRu: "Доллар США", Symbol: "$"},
		{Code: "VND", Name: "Vietnamese Dong", NameRu: "Вьетнамский донг", Symbol: "₫"},
	}
	for id, name := range map[int64]string{1: "Жильё", 3: "Кафе и рестораны", 7: "Продукты", 10: "Прочее"} {
		s.categories[id] = Category{ID: id, Name: name, Keywords: []string{}}
	}
	return s
}

func (s *memStore) putTrip(t Trip) Trip {
	if t.ID == uuid.Nil {
		t.ID = uuid.Must(uuid.NewV7())
	}
	if t.Status == 0 {
		t.Status = StatusActive
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trips[t.ID] = t
	return t
}

func (s *memStore) trip(id uuid.UUID) Trip {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.trips[id]
}

func (s *memStore) putExpense(e Expense) Expense {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e.ID == 0 {
		e.ID = s.nextExpenseID
	}
	if e.ID >= s.nextExpenseID {
		s.nextExpenseID = e.ID + 1
	}
	s.expenses[e.ID] = e
	return e
}

func mustErr(err error) *Error {
	var e *Error
	if !errors.As(err, &e) {
		return nil
	}
	return e
}
