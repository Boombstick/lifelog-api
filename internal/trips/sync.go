package trips

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
)

type SyncService struct {
	store    Store
	trips    *TripService
	expenses *ExpenseService
	clock    Clock
	logger   *slog.Logger
}

func NewSyncService(store Store, trips *TripService, expenses *ExpenseService, clock Clock, logger *slog.Logger) *SyncService {
	return &SyncService{store: store, trips: trips, expenses: expenses, clock: clock, logger: logger}
}

var errSyncConflict = errors.New("trips: sync conflict")

var syncOperationTypes = map[string]bool{
	OpTripCreate:      true,
	OpTripArrive:      true,
	OpTripEnd:         true,
	OpTripSetCurrency: true,
	OpTripSetTimezone: true,
	OpTripDelete:      true,
	OpExpenseAdd:      true,
}

const (
	syncResolutionSame = "same"
	syncResolutionNew  = "new"
)

func (s *SyncService) Process(ctx context.Context, actor Actor, req SyncRequest) (SyncResponse, error) {
	if err := validateSyncRequest(req); err != nil {
		return SyncResponse{}, err
	}

	results := make([]json.RawMessage, 0, len(*req.Operations))
	blocked := map[uuid.UUID]bool{}
	for _, op := range *req.Operations {
		refs := syncRefs(op)

		stored, err := s.store.SyncResult(ctx, actor.UserID, op.OpID)
		if err != nil {
			return SyncResponse{}, err
		}
		if stored != nil {
			results = append(results, stored)
			continue
		}

		if anyBlocked(blocked, refs) {
			for _, ref := range refs {
				blocked[ref] = true
			}
			raw, err := json.Marshal(SyncResult{OpID: op.OpID, Status: SyncStatusBlocked})
			if err != nil {
				return SyncResponse{}, err
			}
			results = append(results, raw)
			continue
		}

		raw, status, err := s.apply(ctx, actor, op)
		if err != nil {
			return SyncResponse{}, err
		}
		if status == SyncStatusConflict || status == SyncStatusError {
			for _, ref := range refs {
				blocked[ref] = true
			}
		}
		results = append(results, raw)
	}

	response := SyncResponse{Results: results}
	active, err := s.trips.ActiveTrip(ctx, actor)
	if err != nil {
		return SyncResponse{}, err
	}
	if active != nil {
		dto, err := s.trips.TripDTO(ctx, actor, active.ID)
		if err != nil {
			return SyncResponse{}, err
		}
		response.ActiveTrip = &dto
	}
	return response, nil
}

func validateSyncRequest(req SyncRequest) error {
	if req.Operations == nil {
		return Validation(msgSyncNoOperations)
	}
	for _, op := range *req.Operations {
		if op == nil || op.OpID == uuid.Nil {
			return Validation("У каждой операции должен быть op_id")
		}
		if strings.TrimSpace(op.Type) == "" {
			return Validationf("У операции %s не указан тип", op.OpID)
		}
		if !syncOperationTypes[op.Type] {
			return Validationf("У операции %s неизвестный тип '%s'", op.OpID, op.Type)
		}
		if op.OccurredAt == nil {
			return Validationf("У операции %s не указано время occurred_at", op.OpID)
		}
	}
	return nil
}

func syncRefs(op *SyncOperation) []uuid.UUID {
	var refs []uuid.UUID
	add := func(id *uuid.UUID) {
		if id != nil && *id != uuid.Nil {
			refs = append(refs, *id)
		}
	}
	tripClientID := func(ref *SyncTripRef) *uuid.UUID {
		if ref == nil {
			return nil
		}
		return ref.ClientID
	}

	switch op.Type {
	case OpTripCreate:
		var p SyncTripCreatePayload
		if decodeSyncPayload(op.Payload, &p) == nil {
			add(p.ClientID)
		}
	case OpExpenseAdd:
		var p SyncExpenseAddPayload
		if decodeSyncPayload(op.Payload, &p) == nil {
			add(tripClientID(p.Trip))
		}
	case OpTripArrive:
		var p SyncTripArrivePayload
		if decodeSyncPayload(op.Payload, &p) == nil {
			add(tripClientID(p.Trip))
			add(p.NewTripClientID)
		}
	case OpTripEnd:
		var p SyncTripEndPayload
		if decodeSyncPayload(op.Payload, &p) == nil {
			add(tripClientID(p.Trip))
		}
	case OpTripSetCurrency:
		var p SyncTripSetCurrencyPayload
		if decodeSyncPayload(op.Payload, &p) == nil {
			add(tripClientID(p.Trip))
		}
	case OpTripSetTimezone:
		var p SyncTripSetTimezonePayload
		if decodeSyncPayload(op.Payload, &p) == nil {
			add(tripClientID(p.Trip))
		}
	case OpTripDelete:
		var p SyncTripDeletePayload
		if decodeSyncPayload(op.Payload, &p) == nil {
			add(tripClientID(p.Trip))
		}
	}
	return refs
}

func anyBlocked(blocked map[uuid.UUID]bool, refs []uuid.UUID) bool {
	for _, ref := range refs {
		if blocked[ref] {
			return true
		}
	}
	return false
}

func (s *SyncService) apply(ctx context.Context, actor Actor, op *SyncOperation) (json.RawMessage, string, error) {
	var (
		result SyncResult
		stored json.RawMessage

		photoFiles []string
	)

	category := s.precomputeCategory(ctx, op)
	err := s.store.InTx(ctx, func(tx Store) error {
		r, err := s.execute(ctx, actor, tx, op, &photoFiles, category)
		if err != nil {
			return err
		}
		r.OpID = op.OpID
		result = r
		if r.Status == SyncStatusConflict {
			return errSyncConflict
		}
		raw, err := json.Marshal(r)
		if err != nil {
			return err
		}
		if err := tx.SaveSyncResult(ctx, actor.UserID, op.OpID, raw, s.clock.Now()); err != nil {
			return err
		}
		stored = raw
		return nil
	})

	if err == nil {
		s.trips.RemovePhotoFiles(ctx, photoFiles)
		return stored, SyncStatusOK, nil
	}

	if winner, readErr := s.store.SyncResult(ctx, actor.UserID, op.OpID); readErr == nil && winner != nil {
		return winner, SyncStatusOK, nil
	}

	if errors.Is(err, errSyncConflict) {
		raw, marshalErr := json.Marshal(result)
		return raw, SyncStatusConflict, marshalErr
	}

	message, retryable := classifySyncError(err)
	if _, business := AsError(err); !business {
		s.log().WarnContext(ctx, "sync operation failed",
			"op_id", op.OpID, "type", op.Type, "retryable", retryable, "error", err)
	}
	raw, marshalErr := json.Marshal(SyncResult{
		OpID:      op.OpID,
		Status:    SyncStatusError,
		Error:     &message,
		Retryable: &retryable,
	})
	return raw, SyncStatusError, marshalErr
}

func classifySyncError(err error) (message string, retryable bool) {
	if business, ok := AsError(err); ok {
		return business.Message, business.Retryable
	}
	if integrity, ok := AsIntegrityError(err); ok && IsIntegrityClass(integrity.SQLState) {
		return IntegrityMessage(integrity), false
	}
	if state, ok := sqlStateOf(err); ok && IsDataExceptionClass(state) {
		return DataExceptionMessage(state), false
	}
	return MsgSyncRetry, true
}

func (s *SyncService) precomputeCategory(ctx context.Context, op *SyncOperation) *CategoryRef {
	if op.Type != OpExpenseAdd || !isJSONObject(op.Payload) {
		return nil
	}
	var p SyncExpenseAddPayload
	if err := decodeSyncPayload(op.Payload, &p); err != nil || syncBlank(p.Description) {
		return nil
	}
	category := s.expenses.ResolveCategory(ctx, *p.Description)
	return &category
}

func (s *SyncService) execute(ctx context.Context, actor Actor, tx Store, op *SyncOperation, photoFiles *[]string, category *CategoryRef) (SyncResult, error) {
	if !isJSONObject(op.Payload) {
		return SyncResult{}, Validation(msgSyncPayloadNotObject)
	}
	c := syncCall{
		ctx:        ctx,
		actor:      actor,
		tx:         tx,
		trips:      s.trips.WithStore(tx),
		expenses:   s.expenses.WithStore(tx),
		occurredAt: op.OccurredAt.Time,
		now:        s.clock.Now(),
		photoFiles: photoFiles,
		category:   category,
	}
	switch op.Type {
	case OpTripCreate:
		var p SyncTripCreatePayload
		if err := decodeSyncPayload(op.Payload, &p); err != nil {
			return SyncResult{}, err
		}
		return c.tripCreate(p)
	case OpTripArrive:
		var p SyncTripArrivePayload
		if err := decodeSyncPayload(op.Payload, &p); err != nil {
			return SyncResult{}, err
		}
		return c.tripArrive(p)
	case OpTripEnd:
		var p SyncTripEndPayload
		if err := decodeSyncPayload(op.Payload, &p); err != nil {
			return SyncResult{}, err
		}
		return c.tripEnd(p)
	case OpTripSetCurrency:
		var p SyncTripSetCurrencyPayload
		if err := decodeSyncPayload(op.Payload, &p); err != nil {
			return SyncResult{}, err
		}
		return c.tripSetCurrency(p)
	case OpTripSetTimezone:
		var p SyncTripSetTimezonePayload
		if err := decodeSyncPayload(op.Payload, &p); err != nil {
			return SyncResult{}, err
		}
		return c.tripSetTimezone(p)
	case OpTripDelete:
		var p SyncTripDeletePayload
		if err := decodeSyncPayload(op.Payload, &p); err != nil {
			return SyncResult{}, err
		}
		return c.tripDelete(p)
	case OpExpenseAdd:
		var p SyncExpenseAddPayload
		if err := decodeSyncPayload(op.Payload, &p); err != nil {
			return SyncResult{}, err
		}
		return c.expenseAdd(p)
	}

	return SyncResult{}, Validationf("У операции %s неизвестный тип '%s'", op.OpID, op.Type)
}

func isJSONObject(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && trimmed[0] == '{'
}

func decodeSyncPayload(raw json.RawMessage, into any) error {
	if !isJSONObject(raw) {
		return Validation(msgSyncPayloadNotObject)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return malformedPayload(err)
	}
	return nil
}

type syncCall struct {
	ctx        context.Context
	actor      Actor
	tx         Store
	trips      *TripService
	expenses   *ExpenseService
	occurredAt time.Time
	now        time.Time

	photoFiles *[]string

	category *CategoryRef
}

func syncSource() *Source {
	source := SourceApp
	return &source
}

func (c *syncCall) ok(tripID uuid.UUID) (SyncResult, error) {
	dto, err := c.trips.TripDTO(c.ctx, c.actor, tripID)
	if err != nil {
		return SyncResult{}, err
	}
	return SyncResult{Status: SyncStatusOK, Trip: &dto}, nil
}

func (c *syncCall) resolveTrip(ref *SyncTripRef) (*Trip, error) {
	if ref == nil || (ref.ID == nil && ref.ClientID == nil) {
		return nil, Validation("Не указана поездка")
	}
	tripID := ref.ID
	if tripID == nil {
		resolved, err := c.tx.TripIDByClientID(c.ctx, c.actor.UserID, *ref.ClientID)
		if err != nil {
			return nil, err
		}
		if resolved == nil {
			return nil, NotFound(MsgTripNotFound)
		}
		tripID = resolved
	}
	trip, err := c.tx.OwnedTrip(c.ctx, c.actor.UserID, *tripID)
	if err != nil {
		return nil, err
	}
	if trip == nil {
		return nil, NotFound(MsgTripNotFound)
	}
	return trip, nil
}

func (c *syncCall) tripCreate(p SyncTripCreatePayload) (SyncResult, error) {
	if p.ClientID == nil || *p.ClientID == uuid.Nil {
		return SyncResult{}, Validation("Не указан client_id")
	}
	if syncBlank(p.Name) {
		return SyncResult{}, Validation("Укажите название поездки")
	}
	if p.Resolution != nil && *p.Resolution != syncResolutionSame && *p.Resolution != syncResolutionNew {
		return SyncResult{}, Validationf("Неизвестное решение конфликта '%s'", *p.Resolution)
	}

	known, err := c.tx.TripIDByClientID(c.ctx, c.actor.UserID, *p.ClientID)
	if err != nil {
		return SyncResult{}, err
	}
	if known != nil {
		return c.ok(*known)
	}

	var country *Country
	if !syncBlank(p.CountryName) {
		country, err = countryByName(c.ctx, c.tx, *p.CountryName)
		if err != nil {
			return SyncResult{}, err
		}
	}
	var zone *string
	if country != nil {
		zone = &country.DefaultTimezone
	}
	startDate, err := LocalDate(c.occurredAt, zone)
	if err != nil {
		return SyncResult{}, err
	}

	if p.Resolution != nil && *p.Resolution == syncResolutionSame {
		return c.sameTrip(p, country)
	}

	active, err := c.trips.ActiveTrip(c.ctx, c.actor)
	if err != nil {
		return SyncResult{}, err
	}
	var closedTripID *uuid.UUID
	if active != nil {
		if p.Resolution == nil && IsSimilarTrip(*active, country, p.City) {
			existing, err := c.trips.TripDTO(c.ctx, c.actor, active.ID)
			if err != nil {
				return SyncResult{}, err
			}
			return SyncResult{
				Status:   SyncStatusConflict,
				Conflict: &SyncConflict{Kind: SyncConflictSimilarActiveTrip, Existing: existing},
			}, nil
		}
		if _, err := c.trips.End(c.ctx, c.actor, EndTripCommand{TripID: &active.ID, On: &startDate}); err != nil {
			return SyncResult{}, err
		}
		id := active.ID
		closedTripID = &id
	}

	created, err := c.trips.Create(c.ctx, c.actor, CreateTripCommand{
		Name:         *p.Name,
		CountryName:  p.CountryName,
		City:         p.City,
		Budget:       p.Budget,
		CurrencyCode: p.CurrencyCode,
		StartDate:    &startDate,
		Source:       syncSource(),
	})
	if err != nil {
		return SyncResult{}, err
	}
	if err := c.tx.InsertTripClientID(c.ctx, c.actor.UserID, *p.ClientID, created.ID); err != nil {
		return SyncResult{}, err
	}
	result, err := c.ok(created.ID)
	if err != nil {
		return SyncResult{}, err
	}
	result.ClosedTripID = closedTripID
	return result, nil
}

func (c *syncCall) sameTrip(p SyncTripCreatePayload, country *Country) (SyncResult, error) {
	if p.SameAsTripID == nil {
		return SyncResult{}, Validation("Для решения 'same' нужен same_as_trip_id")
	}
	trip, err := c.tx.OwnedTrip(c.ctx, c.actor.UserID, *p.SameAsTripID)
	if err != nil {
		return SyncResult{}, err
	}
	if trip == nil {
		return SyncResult{}, NotFound(MsgTripNotFound)
	}

	if syncBlank(trip.CurrencyCode) && !syncBlank(p.CurrencyCode) {
		currency, err := c.tx.CurrencyByCode(c.ctx, strings.ToUpper(*p.CurrencyCode))
		if err != nil {
			return SyncResult{}, err
		}
		if currency == nil {
			return SyncResult{}, UnknownCurrency(*p.CurrencyCode)
		}
	}
	FillBlanks(trip, p, country)
	if err := checkCity(trip.City); err != nil {
		return SyncResult{}, err
	}
	trip.UpdatedAt = c.now
	if err := c.tx.UpdateTrip(c.ctx, trip); err != nil {
		return SyncResult{}, err
	}
	if err := c.tx.InsertTripClientID(c.ctx, c.actor.UserID, *p.ClientID, trip.ID); err != nil {
		return SyncResult{}, err
	}
	return c.ok(trip.ID)
}

func (c *syncCall) tripArrive(p SyncTripArrivePayload) (SyncResult, error) {
	if syncBlank(p.CountryName) {
		return SyncResult{}, Validation("Укажите страну")
	}
	trip, err := c.resolveTrip(p.Trip)
	if err != nil {
		return SyncResult{}, err
	}
	date, err := LocalDate(c.occurredAt, trip.Timezone)
	if err != nil {
		return SyncResult{}, err
	}
	arrival, err := c.trips.Arrive(c.ctx, c.actor, ArriveCommand{
		CountryName: *p.CountryName,
		City:        p.City,
		TripID:      &trip.ID,
		On:          &date,
		Source:      syncSource(),
	})
	if err != nil {
		return SyncResult{}, err
	}
	if arrival.StartedNewTrip && p.NewTripClientID != nil && *p.NewTripClientID != uuid.Nil {
		if err := c.tx.InsertTripClientID(c.ctx, c.actor.UserID, *p.NewTripClientID, arrival.TripID); err != nil {
			return SyncResult{}, err
		}
	}
	result, err := c.ok(arrival.TripID)
	if err != nil {
		return SyncResult{}, err
	}
	result.Arrival = &arrival
	if arrival.StartedNewTrip {
		closed := trip.ID
		result.ClosedTripID = &closed
	}
	return result, nil
}

func (c *syncCall) tripEnd(p SyncTripEndPayload) (SyncResult, error) {
	trip, err := c.resolveTrip(p.Trip)
	if err != nil {
		return SyncResult{}, err
	}

	if trip.Status != StatusCompleted {
		date, err := LocalDate(c.occurredAt, trip.Timezone)
		if err != nil {
			return SyncResult{}, err
		}
		if _, err := c.trips.End(c.ctx, c.actor, EndTripCommand{TripID: &trip.ID, On: &date}); err != nil {
			return SyncResult{}, err
		}
	}
	return c.ok(trip.ID)
}

func (c *syncCall) tripSetCurrency(p SyncTripSetCurrencyPayload) (SyncResult, error) {
	if syncBlank(p.CurrencyCode) {
		return SyncResult{}, Validation("Укажите валюту")
	}
	trip, err := c.resolveTrip(p.Trip)
	if err != nil {
		return SyncResult{}, err
	}
	if _, err := c.trips.SetCurrency(c.ctx, c.actor, SetCurrencyCommand{
		Currency: strings.ToUpper(*p.CurrencyCode),
		TripID:   &trip.ID,
	}); err != nil {
		return SyncResult{}, err
	}
	return c.ok(trip.ID)
}

func (c *syncCall) tripSetTimezone(p SyncTripSetTimezonePayload) (SyncResult, error) {
	if p.Latitude == nil || p.Longitude == nil {
		return SyncResult{}, Validation("Нужны широта и долгота")
	}
	trip, err := c.resolveTrip(p.Trip)
	if err != nil {
		return SyncResult{}, err
	}
	if _, err := c.trips.SetTimezone(c.ctx, c.actor, SetTimezoneCommand{
		Latitude:  float64(*p.Latitude),
		Longitude: float64(*p.Longitude),
		TripID:    &trip.ID,
	}); err != nil {
		return SyncResult{}, err
	}
	return c.ok(trip.ID)
}

func (c *syncCall) tripDelete(p SyncTripDeletePayload) (SyncResult, error) {
	ref := p.Trip
	if ref == nil || (ref.ID == nil && ref.ClientID == nil) {
		return SyncResult{}, Validation("Не указана поездка")
	}
	trip, err := c.resolveTrip(ref)
	if err != nil {
		if business, ok := AsError(err); ok && business.Kind == KindNotFound {
			return SyncResult{Status: SyncStatusOK, DeletedTripID: ref.ID}, nil
		}
		return SyncResult{}, err
	}
	deleted, err := c.trips.DeleteRows(c.ctx, c.actor, trip.ID)
	if err != nil {
		return SyncResult{}, err
	}
	if c.photoFiles != nil {
		*c.photoFiles = append(*c.photoFiles, deleted.PhotoFiles...)
	}
	id := deleted.TripID
	return SyncResult{Status: SyncStatusOK, DeletedTripID: &id}, nil
}

func (c *syncCall) expenseAdd(p SyncExpenseAddPayload) (SyncResult, error) {
	if p.ClientID == nil || *p.ClientID == uuid.Nil {
		return SyncResult{}, Validation("Не указан client_id")
	}
	if p.Amount == nil {
		return SyncResult{}, Validation("Укажите сумму")
	}
	if syncBlank(p.Description) {
		return SyncResult{}, Validation("Укажите описание траты")
	}
	trip, err := c.resolveTrip(p.Trip)
	if err != nil {
		return SyncResult{}, err
	}
	occurredAt := c.occurredAt
	date := occurredAt.UTC()
	added, err := c.expenses.Add(c.ctx, c.actor, AddExpenseCommand{
		Amount:      *p.Amount,
		Currency:    p.Currency,
		Description: *p.Description,
		Date:        &date,
		TripID:      &trip.ID,
		ClientID:    p.ClientID,
		OccurredAt:  &occurredAt,
		Source:      syncSource(),
		Category:    c.category,
	})
	if err != nil {
		return SyncResult{}, err
	}
	dto, err := c.expenses.ExpenseDTO(c.ctx, c.actor, added.ID)
	if err != nil {
		return SyncResult{}, err
	}
	return SyncResult{Status: SyncStatusOK, Expense: &dto}, nil
}

func syncBlank(s *string) bool {
	return s == nil || strings.TrimSpace(*s) == ""
}

func (s *SyncService) log() *slog.Logger {
	if s.logger == nil {
		return slog.Default()
	}
	return s.logger
}

const (
	msgSyncNoOperations     = "Нет списка операций"
	msgSyncPayloadNotObject = "Данные операции должны быть объектом"
	msgSyncPayloadMalformed = "Данные операции не разбираются"
)

func malformedPayload(err error) error {
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) && typeErr.Field != "" {
		return Validationf("%s: неверный тип поля «%s»", msgSyncPayloadMalformed, typeErr.Field)
	}
	return Validation(msgSyncPayloadMalformed)
}
