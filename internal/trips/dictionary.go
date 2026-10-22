package trips

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"unicode/utf8"
)

type DictionaryService struct {
	store  Store
	index  CategoryIndex
	logger *slog.Logger

	mu         sync.Mutex
	countries  []CountryDTO
	currencies []CurrencyDTO
	codes      map[string]struct{}
}

func NewDictionaryService(store Store, index CategoryIndex, logger *slog.Logger) *DictionaryService {
	return &DictionaryService{store: store, index: index, logger: logger}
}

const msgSaveEntityChanges = "Не удалось сохранить категорию: такое название или ключевое слово уже есть"

func (s *DictionaryService) Seed(ctx context.Context, data SeedData) error {
	if err := s.store.SeedDictionaries(ctx, data); err != nil {
		return err
	}
	s.resetCache()

	if err := s.index.Ensure(ctx); err != nil {
		s.log().ErrorContext(ctx, "category index: ensure failed", "error", err)
		return nil
	}
	categories, err := s.store.Categories(ctx)
	if err != nil {
		return err
	}
	if err := s.index.SyncAll(ctx, categories); err != nil {
		s.log().ErrorContext(ctx, "category index: sync failed", "error", err)
	}
	return nil
}

func (s *DictionaryService) Countries(ctx context.Context) (CountriesResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.countries == nil {
		rows, err := s.store.Countries(ctx)
		if err != nil {
			return CountriesResponse{}, err
		}
		countries := make([]CountryDTO, 0, len(rows))
		for _, c := range rows {
			countries = append(countries, CountryDTO{Code: c.Code, Name: c.Name, NameRu: c.NameRu})
		}
		s.countries = countries
	}
	return CountriesResponse{Result: s.countries}, nil
}

func (s *DictionaryService) Currencies(ctx context.Context) (CurrenciesResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadCurrencies(ctx); err != nil {
		return CurrenciesResponse{}, err
	}
	return CurrenciesResponse{Result: s.currencies}, nil
}

func (s *DictionaryService) KnownCurrency(ctx context.Context, code string) (bool, error) {
	if strings.TrimSpace(code) == "" {
		return false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadCurrencies(ctx); err != nil {
		return false, err
	}
	_, ok := s.codes[strings.ToUpper(code)]
	return ok, nil
}

func (s *DictionaryService) loadCurrencies(ctx context.Context) error {
	if s.currencies != nil {
		return nil
	}
	rows, err := s.store.Currencies(ctx)
	if err != nil {
		return err
	}
	currencies := make([]CurrencyDTO, 0, len(rows))
	codes := make(map[string]struct{}, len(rows))
	for _, c := range rows {
		currencies = append(currencies, CurrencyDTO{Code: c.Code, Name: c.Name, NameRu: c.NameRu, Symbol: c.Symbol})
		codes[c.Code] = struct{}{}
	}
	s.currencies = currencies
	s.codes = codes
	return nil
}

func (s *DictionaryService) resetCache() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.countries = nil
	s.currencies = nil
	s.codes = nil
}

func (s *DictionaryService) Categories(ctx context.Context, includeKeywords bool) ([]CategoryDTO, error) {
	rows, err := s.store.Categories(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]CategoryDTO, 0, len(rows))
	for _, c := range rows {
		dto := CategoryDTO{Name: c.Name, ID: c.ID}
		if includeKeywords {
			dto.Keywords = c.Keywords
			if dto.Keywords == nil {
				dto.Keywords = []string{}
			}
		}
		result = append(result, dto)
	}
	return result, nil
}

func (s *DictionaryService) CreateCategory(ctx context.Context, actor Actor, req CreateCategoryRequest) (int64, error) {
	if err := canWriteCategories(actor); err != nil {
		return 0, err
	}
	if utf8.RuneCountInString(req.Name) > MaxCategoryNameLen {
		return 0, FieldTooLong("name", MaxCategoryNameLen)
	}
	keywords := req.Keywords
	if keywords == nil {
		keywords = []string{}
	}
	category := &Category{Name: req.Name, Keywords: keywords}
	if err := s.store.InsertCategory(ctx, category); err != nil {
		if _, ok := AsIntegrityError(err); ok {
			return 0, Conflict(msgSaveEntityChanges)
		}
		return 0, err
	}

	if err := s.index.Put(ctx, *category); err != nil {
		s.log().WarnContext(ctx, "category index: put failed", "id", category.ID, "error", err)
	}
	return category.ID, nil
}

func (s *DictionaryService) AddKeywords(ctx context.Context, actor Actor, id int64, keywords []string) error {
	if err := canWriteCategories(actor); err != nil {
		return err
	}
	if len(keywords) == 0 {
		return nil
	}
	category, err := s.store.CategoryByID(ctx, id)
	if err != nil {
		return err
	}
	if category == nil {
		return Validationf("Категории с Id = %d не найдено", id)
	}

	existing := make(map[string]struct{}, len(category.Keywords))
	for _, k := range category.Keywords {
		existing[strings.ToLower(k)] = struct{}{}
	}
	var added []string
	seen := map[string]struct{}{}
	for _, k := range keywords {
		lower := strings.ToLower(k)
		if _, dup := seen[lower]; dup {
			continue
		}
		seen[lower] = struct{}{}
		if _, present := existing[lower]; present {
			continue
		}
		added = append(added, lower)
	}
	if len(added) == 0 {
		return nil
	}

	updated := make([]string, 0, len(category.Keywords)+len(added))
	updated = append(updated, category.Keywords...)
	updated = append(updated, added...)
	if err := s.store.SetCategoryKeywords(ctx, id, updated); err != nil {
		if _, ok := AsIntegrityError(err); ok {
			return Validation(msgSaveEntityChanges)
		}
		return err
	}
	if err := s.index.AddKeywords(ctx, id, added); err != nil {
		s.log().WarnContext(ctx, "category index: add keywords failed", "id", id, "error", err)
	}
	return nil
}

func canWriteCategories(actor Actor) error {
	if actor.Mode == ModeBot || actor.Mode == ModeInternal {
		return nil
	}
	return Forbidden(MsgForbidden)
}

func (s *DictionaryService) log() *slog.Logger {
	if s.logger == nil {
		return slog.Default()
	}
	return s.logger
}
