package trips

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

type TripService struct {
	store Store
	tz    TimezoneLookup

	files  PhotoStore
	clock  Clock
	logger *slog.Logger
}

func NewTripService(store Store, tz TimezoneLookup, files PhotoStore, clock Clock, logger *slog.Logger) *TripService {
	return &TripService{store: store, tz: tz, files: files, clock: clock, logger: logger}
}

func (s *TripService) WithStore(store Store) *TripService {
	copied := *s
	copied.store = store
	return &copied
}

type CreateTripCommand struct {
	Name         string
	CountryName  *string
	City         *string
	Budget       *Decimal
	CurrencyCode *string

	StartDate *Date

	Source *Source
}

type ArriveCommand struct {
	CountryName string
	City        *string

	TripID *uuid.UUID

	On     *Date
	Source *Source
}

type EndTripCommand struct {
	TripID *uuid.UUID
	On     *Date
}

type SetCurrencyCommand struct {
	Currency string
	TripID   *uuid.UUID
}

type SetTimezoneCommand struct {
	Latitude  float64
	Longitude float64
	TripID    *uuid.UUID
}

func (s *TripService) History(ctx context.Context, actor Actor) (TripHistoryResponse, error) {
	rows, err := s.store.TripHistory(ctx, actor.UserID)
	if err != nil {
		return TripHistoryResponse{}, err
	}
	today := s.today()
	items := make([]TripHistoryItem, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		items = append(items, TripHistoryItem{
			ID:           row.ID,
			Name:         row.Name,
			Status:       row.Status,
			IsActive:     row.Status == StatusActive,
			Budget:       row.Budget,
			StartDate:    row.StartDate,
			EndDate:      row.EndDate,
			DurationDays: row.DurationDays(today),
			CountryCode:  row.CountryCode,
			CountryName:  row.CountryName,
			City:         row.City,
			CurrencyCode: row.CurrencyCode,
			TotalUSD:     row.TotalUSD,
			ExpenseCount: row.ExpenseCount,
		})
	}
	return TripHistoryResponse{Items: items}, nil
}

func (s *TripService) Active(ctx context.Context, actor Actor) (ActiveTripResponse, error) {
	trip, err := s.ActiveTrip(ctx, actor)
	if err != nil {
		return ActiveTripResponse{}, err
	}
	if trip == nil {
		return ActiveTripResponse{HasActiveTrip: false}, nil
	}
	return ActiveTripResponse{
		HasActiveTrip: true,
		Trip: &ActiveTripDTO{
			ID:           trip.ID,
			Name:         trip.Name,
			Budget:       trip.Budget,
			StartDate:    trip.StartDate,
			DurationDays: trip.DurationDays(s.today()),
			CountryCode:  trip.CountryCode,
			CountryName:  trip.CountryName,
			City:         trip.City,
			CurrencyCode: trip.CurrencyCode,
			Timezone:     trip.Timezone,
		},
	}, nil
}

func (s *TripService) Create(ctx context.Context, actor Actor, cmd CreateTripCommand) (CreateTripResponse, error) {
	active, err := s.ActiveTrip(ctx, actor)
	if err != nil {
		return CreateTripResponse{}, err
	}
	if active != nil {
		return CreateTripResponse{}, Conflict(fmt.Sprintf(MsgActiveTripExists, active.Name))
	}
	if err := checkCity(cmd.City); err != nil {
		return CreateTripResponse{}, err
	}

	trip := &Trip{
		UserID: actor.UserID,
		Name:   cmd.Name,
		Budget: cmd.Budget,
		City:   cmd.City,
		Status: StatusPlanned,
		Source: sourceOf(actor, cmd.Source),
	}

	if !isBlank(cmd.CurrencyCode) {
		code := strings.ToUpper(*cmd.CurrencyCode)

		currency, err := s.store.CurrencyByCode(ctx, code)
		if err != nil {
			return CreateTripResponse{}, err
		}
		if currency == nil {
			return CreateTripResponse{}, UnknownCurrency(*cmd.CurrencyCode)
		}
		trip.CurrencyCode = &code
	}

	var countryName *string
	if !isBlank(cmd.CountryName) {
		country, err := countryByName(ctx, s.store, *cmd.CountryName)
		if err != nil {
			return CreateTripResponse{}, err
		}
		trip.SetCountry(*country)
		name := country.NameRu
		countryName = &name
	}

	if err := trip.Start(cmd.StartDate, s.today()); err != nil {
		return CreateTripResponse{}, err
	}
	if err := s.insert(ctx, actor, trip); err != nil {
		return CreateTripResponse{}, err
	}

	return CreateTripResponse{
		ID:           trip.ID,
		Name:         trip.Name,
		CountryName:  countryName,
		City:         trip.City,
		CurrencyCode: trip.CurrencyCode,
		StartDate:    trip.StartDate,
	}, nil
}

func (s *TripService) End(ctx context.Context, actor Actor, cmd EndTripCommand) (EndTripResponse, error) {
	trip, err := resolveTrip(ctx, s.store, actor, cmd.TripID, MsgNoActiveTrips)
	if err != nil {
		return EndTripResponse{}, err
	}
	if err := trip.End(cmd.On, s.today()); err != nil {
		return EndTripResponse{}, err
	}
	if err := s.update(ctx, actor, trip); err != nil {
		return EndTripResponse{}, err
	}
	start := trip.StartDate
	if start == nil {
		start = trip.EndDate
	}
	return EndTripResponse{Name: trip.Name, StartDate: start, EndDate: trip.EndDate}, nil
}

func (s *TripService) Arrive(ctx context.Context, actor Actor, cmd ArriveCommand) (ArriveResponse, error) {
	trip, err := resolveTrip(ctx, s.store, actor, cmd.TripID, MsgNoActiveTrip)
	if err != nil {
		return ArriveResponse{}, err
	}
	country, err := countryByName(ctx, s.store, cmd.CountryName)
	if err != nil {
		return ArriveResponse{}, err
	}

	code := country.Code
	if equalFoldNullable(trip.CountryCode, &code) && equalFoldNullable(trip.City, cmd.City) {
		return ArriveResponse{}, Conflict("Вы уже находитесь здесь: " + country.NameRu)
	}
	if err := checkCity(cmd.City); err != nil {
		return ArriveResponse{}, err
	}

	if isBlank(trip.CountryCode) {
		trip.SetCountry(*country)
		if !isBlank(cmd.City) {
			city := *cmd.City
			trip.City = &city
		}
		if err := s.update(ctx, actor, trip); err != nil {
			return ArriveResponse{}, err
		}
		return s.describeArrival(ctx, trip.ID, country, trip.City, false)
	}

	next := &Trip{
		UserID: actor.UserID,
		Name:   trip.Name,
		Budget: trip.Budget,
		City:   cmd.City,
		Status: StatusPlanned,
		Source: sourceOf(actor, cmd.Source),
	}
	next.SetCountry(*country)
	today := s.today()
	err = s.store.InTx(ctx, func(tx Store) error {
		txs := s.WithStore(tx)
		if err := trip.End(cmd.On, today); err != nil {
			return err
		}
		if err := txs.update(ctx, actor, trip); err != nil {
			return err
		}

		if err := next.Start(trip.EndDate, today); err != nil {
			return err
		}
		return txs.insert(ctx, actor, next)
	})
	if err != nil {
		return ArriveResponse{}, err
	}
	return s.describeArrival(ctx, next.ID, country, next.City, true)
}

func (s *TripService) SetCurrency(ctx context.Context, actor Actor, cmd SetCurrencyCommand) (SetCurrencyResponse, error) {
	trip, err := resolveTrip(ctx, s.store, actor, cmd.TripID, MsgNoActiveTrips)
	if err != nil {
		return SetCurrencyResponse{}, err
	}
	currency, err := s.store.CurrencyByCode(ctx, cmd.Currency)
	if err != nil {
		return SetCurrencyResponse{}, err
	}
	if currency == nil {
		return SetCurrencyResponse{}, Validationf(MsgCurrencyNotFound, cmd.Currency)
	}
	code := currency.Code
	trip.CurrencyCode = &code
	if err := s.update(ctx, actor, trip); err != nil {
		return SetCurrencyResponse{}, err
	}
	return SetCurrencyResponse{CurrencyCode: currency.Code, CurrencyName: currency.NameRu}, nil
}

func (s *TripService) SetTimezone(ctx context.Context, actor Actor, cmd SetTimezoneCommand) (SetTimezoneResponse, error) {
	trip, err := resolveTrip(ctx, s.store, actor, cmd.TripID, MsgNoActiveTrips)
	if err != nil {
		return SetTimezoneResponse{}, err
	}
	zone, err := s.tz.Lookup(cmd.Latitude, cmd.Longitude)
	if err != nil {
		return SetTimezoneResponse{}, err
	}

	offset, err := s.tz.BaseOffset(zone, s.clock.Now().Year())
	if err != nil {
		return SetTimezoneResponse{}, err
	}
	if err := trip.SetTimeZone(zone); err != nil {
		return SetTimezoneResponse{}, err
	}
	if err := s.update(ctx, actor, trip); err != nil {
		return SetTimezoneResponse{}, err
	}
	return SetTimezoneResponse{Timezone: FormatUTCOffset(offset)}, nil
}

type DeletedTrip struct {
	TripID     uuid.UUID
	Expenses   int
	Photos     int
	PhotoFiles []string
}

func (s *TripService) Delete(ctx context.Context, actor Actor, tripID uuid.UUID) (DeletedTrip, error) {
	deleted, err := s.DeleteRows(ctx, actor, tripID)
	if err != nil {
		return DeletedTrip{}, err
	}
	s.RemovePhotoFiles(ctx, deleted.PhotoFiles)
	return deleted, nil
}

func (s *TripService) DeleteRows(ctx context.Context, actor Actor, tripID uuid.UUID) (DeletedTrip, error) {
	result := DeletedTrip{TripID: tripID}
	err := s.store.InTx(ctx, func(tx Store) error {
		trip, err := tx.OwnedTrip(ctx, actor.UserID, tripID)
		if err != nil {
			return err
		}
		if trip == nil {
			return NotFound(MsgTripNotFound)
		}

		photos, err := tx.TripPhotos(ctx, actor.UserID, tripID)
		if err != nil {
			return err
		}
		for _, photo := range photos {
			result.PhotoFiles = append(result.PhotoFiles, photo.StoredFileName)
		}

		if result.Expenses, err = tx.DeleteExpensesOfTrip(ctx, tripID); err != nil {
			return err
		}
		if result.Photos, err = tx.DeleteTripPhotos(ctx, tripID); err != nil {
			return err
		}
		found, err := tx.DeleteTrip(ctx, actor.UserID, tripID)
		if err != nil {
			return err
		}
		if !found {
			return NotFound(MsgTripNotFound)
		}
		return nil
	})
	if err != nil {
		return DeletedTrip{}, err
	}
	return result, nil
}

func (s *TripService) RemovePhotoFiles(ctx context.Context, names []string) {
	if s.files == nil {
		return
	}
	for _, name := range names {
		if err := s.files.Delete(ctx, name); err != nil {
			s.log().WarnContext(ctx, "trips: photo file left behind", "file", name, "error", err)
		}
	}
}

func (s *TripService) log() *slog.Logger {
	if s.logger == nil {
		return slog.Default()
	}
	return s.logger
}

func (s *TripService) ActiveTrip(ctx context.Context, actor Actor) (*Trip, error) {
	return activeTrip(ctx, s.store, actor.UserID)
}

func (s *TripService) TripDTO(ctx context.Context, actor Actor, tripID uuid.UUID) (TripDTO, error) {
	trip, err := s.store.OwnedTrip(ctx, actor.UserID, tripID)
	if err != nil {
		return TripDTO{}, err
	}
	if trip == nil {
		return TripDTO{}, NotFound(MsgTripNotFound)
	}
	clientIDs, err := s.store.TripClientIDs(ctx, trip.ID)
	if err != nil {
		return TripDTO{}, err
	}
	if clientIDs == nil {
		clientIDs = []uuid.UUID{}
	}
	return TripDTO{
		ID:           trip.ID,
		Name:         trip.Name,
		Budget:       trip.Budget,
		StartDate:    trip.StartDate,
		DurationDays: trip.DurationDays(s.today()),
		CountryCode:  trip.CountryCode,
		CountryName:  trip.CountryName,
		City:         trip.City,
		CurrencyCode: trip.CurrencyCode,
		Timezone:     trip.Timezone,
		Status:       trip.Status,
		EndDate:      trip.EndDate,
		ClientIDs:    clientIDs,
	}, nil
}

func (s *TripService) today() Date { return DateOf(s.clock.Now()) }

func resolveTrip(ctx context.Context, store Store, actor Actor, tripID *uuid.UUID, noActive string) (*Trip, error) {
	if tripID != nil {
		trip, err := store.OwnedTrip(ctx, actor.UserID, *tripID)
		if err != nil {
			return nil, err
		}
		if trip == nil {
			return nil, NotFound(MsgTripNotFound)
		}
		return trip, nil
	}
	trip, err := activeTrip(ctx, store, actor.UserID)
	if err != nil {
		return nil, err
	}
	if trip == nil {
		return nil, Validation(noActive)
	}
	return trip, nil
}

func activeTrip(ctx context.Context, store Store, userID uuid.UUID) (*Trip, error) {
	trips, err := store.ActiveTrips(ctx, userID)
	if err != nil {
		return nil, err
	}
	switch len(trips) {
	case 0:
		return nil, nil
	case 1:
		return &trips[0], nil
	}
	return nil, Conflict(MsgSequenceMoreThanOne)
}

func countryByName(ctx context.Context, store Store, name string) (*Country, error) {
	country, err := store.CountryByName(ctx, strings.ToLower(strings.TrimSpace(name)))
	if err != nil {
		return nil, err
	}
	if country == nil {
		return nil, Validation(MsgCountryUnsupported)
	}
	return country, nil
}

func (s *TripService) describeArrival(ctx context.Context, tripID uuid.UUID, country *Country, city *string, startedNewTrip bool) (ArriveResponse, error) {
	currency, err := s.store.CurrencyByCode(ctx, country.DefaultCurrencyCode)
	if err != nil {
		return ArriveResponse{}, err
	}
	currencyName := ""
	if currency != nil {
		currencyName = currency.NameRu
	}
	return ArriveResponse{
		TripID:          tripID,
		CountryName:     country.NameRu,
		City:            city,
		CurrencyName:    currencyName,
		CurrencyCode:    country.DefaultCurrencyCode,
		IsMultiCurrency: country.IsMultiCurrency,
		IsMultiTimezone: country.IsMultiTimezone,
		StartedNewTrip:  startedNewTrip,
	}, nil
}

func (s *TripService) insert(ctx context.Context, actor Actor, trip *Trip) error {
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	now := s.clock.Now()
	trip.ID = id
	trip.CreatedAt = now
	trip.UpdatedAt = now
	return s.activeIndexConflict(ctx, actor, trip.Name, s.store.InsertTrip(ctx, trip))
}

func (s *TripService) update(ctx context.Context, actor Actor, trip *Trip) error {
	trip.UpdatedAt = s.clock.Now()
	return s.activeIndexConflict(ctx, actor, trip.Name, s.store.UpdateTrip(ctx, trip))
}

func (s *TripService) activeIndexConflict(ctx context.Context, actor Actor, fallbackName string, err error) error {
	if !errors.Is(err, ErrActiveTripExists) {
		return err
	}
	name := fallbackName
	if trips, readErr := s.store.ActiveTrips(ctx, actor.UserID); readErr == nil && len(trips) > 0 {
		name = trips[0].Name
	}
	return Conflict(fmt.Sprintf(MsgActiveTripExists, name))
}

func checkCity(city *string) error {
	if city != nil && utf8.RuneCountInString(*city) > MaxCityLen {
		return FieldTooLong("city", MaxCityLen)
	}
	return nil
}

func sourceOf(actor Actor, explicit *Source) Source {
	if explicit != nil {
		return *explicit
	}
	return actor.Source()
}
