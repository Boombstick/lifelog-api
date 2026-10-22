package trips

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type ExpenseService struct {
	store      Store
	dictionary *DictionaryService
	categories CategorySearch
	rates      Rates
	clock      Clock
	logger     *slog.Logger
}

func NewExpenseService(store Store, dictionary *DictionaryService, categories CategorySearch, rates Rates, clock Clock, logger *slog.Logger) *ExpenseService {
	return &ExpenseService{
		store:      store,
		dictionary: dictionary,
		categories: categories,
		rates:      rates,
		clock:      clock,
		logger:     logger,
	}
}

func (s *ExpenseService) WithStore(store Store) *ExpenseService {
	copied := *s
	copied.store = store
	return &copied
}

type AddExpenseCommand struct {
	Amount      Decimal
	Currency    *string
	Description string

	Date *time.Time

	TripID *uuid.UUID

	ClientID *uuid.UUID

	OccurredAt *time.Time

	Source *Source

	Category *CategoryRef
}

func (s *ExpenseService) Add(ctx context.Context, actor Actor, cmd AddExpenseCommand) (AddExpenseResponse, error) {
	if !isBlank(cmd.Currency) {
		known, err := s.dictionary.KnownCurrency(ctx, *cmd.Currency)
		if err != nil {
			return AddExpenseResponse{}, err
		}
		if !known {
			return AddExpenseResponse{}, Validation("Валюта " + *cmd.Currency + " не поддерживается")
		}
	}

	if actor.UserID == uuid.Nil {
		return AddExpenseResponse{}, Validation("Пользователь не найден")
	}

	if cmd.ClientID != nil {
		existing, err := s.store.ExpenseByClientID(ctx, *cmd.ClientID)
		if err != nil {
			return AddExpenseResponse{}, err
		}
		if existing != nil {
			if existing.TripUserID != actor.UserID {
				return AddExpenseResponse{}, Validation("Трата с таким client_id уже есть у другого пользователя")
			}
			return AddExpenseResponse{
				CategoryName: existing.CategoryName,
				Amount:       existing.Amount,
				Currency:     existing.CurrencyCode,
				ID:           existing.ID,
			}, nil
		}
	}

	trip, err := resolveTrip(ctx, s.store, actor, cmd.TripID, MsgNoActiveTrip)
	if err != nil {
		return AddExpenseResponse{}, err
	}

	var category CategoryRef
	if cmd.Category != nil {
		category = *cmd.Category
	} else {
		category = s.ResolveCategory(ctx, cmd.Description)
	}

	snapshot, err := s.snapshot(ctx, cmd.OccurredAt)
	if err != nil {
		return AddExpenseResponse{}, err
	}

	currency := USD
	switch {
	case !isBlank(cmd.Currency):
		currency = strings.ToUpper(*cmd.Currency)
	case !isBlank(trip.CurrencyCode):
		currency = *trip.CurrencyCode
	}

	rate, ok := snapshot.Rates[currency]
	if !ok {
		return AddExpenseResponse{}, Validation("Нет курса для валюты " + currency)
	}
	if rate.IsZero() {
		return AddExpenseResponse{}, errors.New("trips: zero exchange rate for " + currency)
	}
	amountUSD := DecimalFrom(cmd.Amount.DivRound(rate.Decimal, 28))

	occurredAt, err := s.occurredDate(cmd, trip)
	if err != nil {
		return AddExpenseResponse{}, err
	}

	if trip.ID == uuid.Nil {
		return AddExpenseResponse{}, Validation("Не указана поездка")
	}
	if category.ID <= 0 {
		return AddExpenseResponse{}, Validation("Не указана категория траты")
	}
	if cmd.Amount.Sign() <= 0 {
		return AddExpenseResponse{}, Validation("Сумма должна быть больше нуля")
	}

	if cmd.Amount.Round(2).GreaterThanOrEqual(maxAmount) {
		return AddExpenseResponse{}, Validation(msgAmountTooLarge)
	}
	if strings.TrimSpace(currency) == "" || utf8.RuneCountInString(currency) != 3 {
		return AddExpenseResponse{}, Validation("Нужен трёхбуквенный код валюты")
	}

	if amountUSD.Sign() <= 0 {
		return AddExpenseResponse{}, Validation("Сумма в долларах должна быть больше нуля")
	}
	if snapshot.ID <= 0 {
		return AddExpenseResponse{}, Validation("Не найден снимок курсов валют")
	}
	if strings.TrimSpace(cmd.Description) == "" {
		return AddExpenseResponse{}, Validation("Укажите описание траты")
	}
	description := strings.TrimSpace(cmd.Description)
	if utf8.RuneCountInString(description) > MaxDescriptionLen {
		return AddExpenseResponse{}, FieldTooLong("description", MaxDescriptionLen)
	}

	expense := &Expense{
		TripID:       trip.ID,
		Amount:       cmd.Amount,
		AmountUSD:    DecimalFrom(amountUSD.RoundBank(6)),
		OccurredAt:   occurredAt,
		Description:  description,
		CurrencyCode: currency,
		SnapshotID:   snapshot.ID,
		CategoryID:   category.ID,
		CreatedAt:    s.clock.Now(),
		ClientID:     cmd.ClientID,
		Source:       sourceOf(actor, cmd.Source),
	}

	original, err := s.store.LookAlike(ctx, LookAlikeQuery{
		TripID:       expense.TripID,
		Amount:       cmd.Amount,
		CurrencyCode: expense.CurrencyCode,
		OccurredAt:   expense.OccurredAt,
		Source:       expense.Source,
	})
	if err != nil {
		return AddExpenseResponse{}, err
	}
	expense.DuplicateOfID = original
	if err := s.store.InsertExpense(ctx, expense); err != nil {
		return AddExpenseResponse{}, err
	}
	attrs := []any{"id", expense.ID, "trip_id", expense.TripID, "currency", expense.CurrencyCode, "source", int(expense.Source)}
	if expense.DuplicateOfID != nil {
		attrs = append(attrs, "duplicate_of", *expense.DuplicateOfID)
	}
	s.log().InfoContext(ctx, "expense added", attrs...)

	return AddExpenseResponse{
		CategoryName: category.Name,
		Amount:       cmd.Amount,
		Currency:     currency,
		ID:           expense.ID,
	}, nil
}

const categorySearchTimeout = 500 * time.Millisecond

var maxAmount = decimal.New(1, 16)

const msgAmountTooLarge = "Слишком большая сумма: не больше 9 999 999 999 999 999,99"

func (s *ExpenseService) ResolveCategory(ctx context.Context, description string) CategoryRef {
	fallback := CategoryRef{ID: OtherCategoryID, Name: OtherCategoryName}
	ctx, cancel := context.WithTimeout(ctx, categorySearchTimeout)
	defer cancel()
	found, err := s.categories.Search(ctx, description)
	if err != nil {
		s.log().WarnContext(ctx, "category search failed, using fallback", "error", err)
		return fallback
	}
	if found == nil {
		return fallback
	}
	return *found
}

func (s *ExpenseService) snapshot(ctx context.Context, occurredAt *time.Time) (*Snapshot, error) {
	if occurredAt != nil {
		snapshot, err := s.rates.Closest(ctx, *occurredAt)
		if err != nil {
			return nil, err
		}
		if snapshot == nil {
			return nil, NoSnapshots(MsgNoSnapshotsClosest)
		}
		return snapshot, nil
	}
	snapshot, err := s.rates.Latest(ctx)
	if err != nil {
		return nil, err
	}
	if snapshot == nil {
		return nil, NoSnapshots(MsgNoSnapshotsLatest)
	}
	return snapshot, nil
}

func (s *ExpenseService) occurredDate(cmd AddExpenseCommand, trip *Trip) (Date, error) {
	if cmd.OccurredAt != nil {
		return LocalDate(*cmd.OccurredAt, trip.Timezone)
	}
	moment := s.clock.Now()
	if cmd.Date != nil {
		moment = *cmd.Date
	}
	moment = moment.UTC()
	if isBlank(trip.Timezone) {
		return DateOf(moment), nil
	}
	return LocalDate(moment, trip.Timezone)
}

type ListExpensesQuery struct {
	TripID     *uuid.UUID
	CategoryID *int64
}

func (s *ExpenseService) List(ctx context.Context, actor Actor, q ListExpensesQuery) (ExpenseListResponse, error) {
	rows, err := s.store.ListExpenses(ctx, ExpenseFilter{UserID: actor.UserID, TripID: q.TripID, CategoryID: q.CategoryID})
	if err != nil {
		return ExpenseListResponse{}, err
	}

	type groupKey struct {
		id   int64
		name string
	}
	index := map[groupKey]int{}
	groups := []ExpenseCategoryGroup{}
	total := Decimal{}
	for i := range rows {
		row := &rows[i]
		key := groupKey{row.CategoryID, row.CategoryName}
		at, ok := index[key]
		if !ok {
			at = len(groups)
			index[key] = at
			groups = append(groups, ExpenseCategoryGroup{
				CategoryID:   row.CategoryID,
				CategoryName: row.CategoryName,
				Items:        []ExpenseListItem{},
			})
		}
		group := &groups[at]
		group.Count++
		group.Items = append(group.Items, ExpenseListItem{
			ID:            row.ID,
			Amount:        row.Amount,
			CurrencyCode:  row.CurrencyCode,
			AmountUSD:     row.AmountUSD,
			Description:   row.Description,
			OccurredAt:    row.OccurredAt,
			DuplicateOfID: row.DuplicateOfID,
		})
		if row.DuplicateOfID == nil {
			group.TotalUSD = DecimalFrom(group.TotalUSD.Add(row.AmountUSD.Decimal))
			total = DecimalFrom(total.Add(row.AmountUSD.Decimal))
		}
	}
	sort.SliceStable(groups, func(i, j int) bool {
		return groups[i].TotalUSD.Cmp(groups[j].TotalUSD.Decimal) > 0
	})

	return ExpenseListResponse{Categories: groups, TotalUSD: total, Count: len(rows)}, nil
}

func (s *ExpenseService) Total(ctx context.Context, actor Actor, r DateRange) (TotalResponse, error) {
	total, err := s.store.TotalAmount(ctx, actor.UserID, r)
	if err != nil {
		return TotalResponse{}, err
	}
	return TotalResponse{TotalExpense: total}, nil
}

func (s *ExpenseService) Duplicates(ctx context.Context, actor Actor) ([]DuplicatePair, error) {
	flagged, err := s.store.FlaggedExpenses(ctx, actor.UserID)
	if err != nil {
		return nil, err
	}
	originals := map[int64]*ExpenseDetail{}
	pairs := []DuplicatePair{}
	for i := range flagged {
		row := &flagged[i]
		if row.DuplicateOfID == nil {
			continue
		}
		originalID := *row.DuplicateOfID
		original, seen := originals[originalID]
		if !seen {
			original, err = s.store.OwnedExpense(ctx, actor.UserID, originalID)
			if err != nil {
				return nil, err
			}
			originals[originalID] = original
		}

		if original == nil {
			continue
		}
		pairs = append(pairs, DuplicatePair{Expense: expenseDTO(row), DuplicateOf: expenseDTO(original)})
	}
	return pairs, nil
}

func (s *ExpenseService) NotDuplicate(ctx context.Context, actor Actor, id int64) error {
	found, err := s.store.ClearDuplicateFlag(ctx, actor.UserID, id)
	if err != nil {
		return err
	}
	if !found {
		return NotFound(MsgExpenseNotFound)
	}
	return nil
}

func (s *ExpenseService) Delete(ctx context.Context, actor Actor, id int64) error {
	found, err := s.store.DeleteExpense(ctx, actor.UserID, id)
	if err != nil {
		return err
	}
	if !found {
		return NotFound(MsgExpenseNotFound)
	}
	return nil
}

func (s *ExpenseService) ExpenseDTO(ctx context.Context, actor Actor, id int64) (ExpenseDTO, error) {
	expense, err := s.store.OwnedExpense(ctx, actor.UserID, id)
	if err != nil {
		return ExpenseDTO{}, err
	}
	if expense == nil {
		return ExpenseDTO{}, NotFound(MsgExpenseNotFound)
	}
	return expenseDTO(expense), nil
}

func expenseDTO(e *ExpenseDetail) ExpenseDTO {
	return ExpenseDTO{
		ID:            e.ID,
		Amount:        e.Amount,
		CurrencyCode:  e.CurrencyCode,
		AmountUSD:     e.AmountUSD,
		Description:   e.Description,
		OccurredAt:    e.OccurredAt,
		DuplicateOfID: e.DuplicateOfID,
		TripID:        e.TripID,
		ClientID:      e.ClientID,
		CategoryID:    e.CategoryID,
		CategoryName:  e.CategoryName,
	}
}

func (s *ExpenseService) log() *slog.Logger {
	if s.logger == nil {
		return slog.Default()
	}
	return s.logger
}
