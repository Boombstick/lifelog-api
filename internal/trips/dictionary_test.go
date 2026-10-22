package trips

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func newDictionaryFixture() (*memStore, *fakeIndex, *DictionaryService) {
	store := seededStore()
	index := &fakeIndex{}
	return store, index, NewDictionaryService(store, index, quietLogger)
}

func TestDictionaryCountriesAndCurrenciesAreCached(t *testing.T) {
	store, _, svc := newDictionaryFixture()

	countries, err := svc.Countries(bg)
	if err != nil || len(countries.Result) != 3 || countries.Result[0] != (CountryDTO{Code: "TH", Name: "Thailand", NameRu: "Таиланд"}) {
		t.Fatalf("got %+v %v", countries, err)
	}
	_, _ = svc.Countries(bg)
	if store.calls["Countries"] != 1 {
		t.Fatalf("countries loaded %d times", store.calls["Countries"])
	}

	currencies, err := svc.Currencies(bg)
	if err != nil || len(currencies.Result) != 5 || currencies.Result[3] != (CurrencyDTO{Code: "USD", Name: "US Dollar", NameRu: "Доллар США", Symbol: "$"}) {
		t.Fatalf("got %+v %v", currencies, err)
	}
	for _, code := range []string{"usd", "Thb", "USD"} {
		if ok, err := svc.KnownCurrency(bg, code); !ok || err != nil {
			t.Errorf("%s unknown", code)
		}
	}
	for _, code := range []string{"", " ", "XYZ", "us"} {
		if ok, _ := svc.KnownCurrency(bg, code); ok {
			t.Errorf("%q known", code)
		}
	}
	_, _ = svc.Currencies(bg)
	if store.calls["Currencies"] != 1 {
		t.Fatalf("currencies loaded %d times", store.calls["Currencies"])
	}

	body, _ := json.Marshal(CountriesResponse{Result: countries.Result[:1]})
	if string(body) != `{"result":[{"code":"TH","name":"Thailand","name_ru":"Таиланд"}]}` {
		t.Fatalf("wire %s", body)
	}
}

func TestDictionaryEmptyListsAreArrays(t *testing.T) {
	svc := NewDictionaryService(newMemStore(), &fakeIndex{}, quietLogger)
	countries, _ := svc.Countries(bg)
	currencies, _ := svc.Currencies(bg)
	categories, _ := svc.Categories(bg, false)
	body, _ := json.Marshal([]any{countries, currencies, categories})
	if string(body) != `[{"result":[]},{"result":[]},[]]` {
		t.Fatalf("got %s", body)
	}
}

func TestDictionaryCategories(t *testing.T) {
	store, _, svc := newDictionaryFixture()
	store.categories[1] = Category{ID: 1, Name: "Жильё", Keywords: []string{"отель", "хостел"}}

	without, err := svc.Categories(bg, false)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(without[:2])
	if string(body) != `[{"name":"Жильё","id":1,"keywords":null},{"name":"Кафе и рестораны","id":3,"keywords":null}]` {
		t.Fatalf("without keywords %s", body)
	}

	with, _ := svc.Categories(bg, true)
	body, _ = json.Marshal(with[:2])
	if string(body) != `[{"name":"Жильё","id":1,"keywords":["отель","хостел"]},{"name":"Кафе и рестораны","id":3,"keywords":[]}]` {
		t.Fatalf("with keywords %s", body)
	}
}

func TestDictionaryCreateCategory(t *testing.T) {
	store, index, svc := newDictionaryFixture()
	req := CreateCategoryRequest{Name: "Спорт", Keywords: []string{"фитнес"}}

	_, err := svc.CreateCategory(bg, appActor(store.putTrip(Trip{}).UserID), req)
	wantErr(t, err, KindForbidden, "Недостаточно прав")
	if store.calls["InsertCategory"] != 0 {
		t.Fatal("forbidden create inserted")
	}

	_, err = svc.CreateCategory(bg, Actor{Mode: ModeInternal}, CreateCategoryRequest{Name: strings.Repeat("я", 51)})
	wantErr(t, err, KindValidation, "Поле name не может быть длиннее 50 символов")

	id, err := svc.CreateCategory(bg, Actor{Mode: ModeBot}, req)
	if err != nil || id != 11 {
		t.Fatalf("got %d %v", id, err)
	}
	if fmt.Sprint(index.put) != fmt.Sprint([]Category{{ID: 11, Name: "Спорт", Keywords: []string{"фитнес"}}}) {
		t.Fatalf("indexed %+v", index.put)
	}

	index.putErr = errors.New("es down")
	id, err = svc.CreateCategory(bg, Actor{Mode: ModeInternal}, CreateCategoryRequest{Name: strings.Repeat("я", 50)})
	if err != nil || id != 12 || store.categories[12].Keywords == nil {
		t.Fatalf("got %d %v %+v", id, err, store.categories[12])
	}

	store.failInsertCategory = &IntegrityError{SQLState: "23505", Message: "duplicate"}
	_, err = svc.CreateCategory(bg, Actor{Mode: ModeBot}, req)
	wantErr(t, err, KindConflict, "Не удалось сохранить категорию: такое название или ключевое слово уже есть")

	boom := errors.New("connection refused")
	store.failInsertCategory = boom
	if _, err := svc.CreateCategory(bg, Actor{Mode: ModeBot}, req); !errors.Is(err, boom) || mustErr(err) != nil {
		t.Fatalf("infrastructure error must stay a 500: %v", err)
	}
}

func TestDictionaryAddKeywords(t *testing.T) {
	store, index, svc := newDictionaryFixture()
	store.categories[1] = Category{ID: 1, Name: "Жильё", Keywords: []string{"Отель"}}
	bot := Actor{Mode: ModeBot}

	wantErr(t, svc.AddKeywords(bg, Actor{Mode: ModeApp}, 1, []string{"x"}), KindForbidden, "Недостаточно прав")
	wantErr(t, svc.AddKeywords(bg, Actor{}, 1, []string{"x"}), KindForbidden, "Недостаточно прав")

	if err := svc.AddKeywords(bg, bot, 404, nil); err != nil || store.calls["CategoryByID"] != 0 {
		t.Fatalf("empty: %v", err)
	}
	wantErr(t, svc.AddKeywords(bg, bot, 404, []string{"x"}), KindValidation, "Категории с Id = 404 не найдено")

	if err := svc.AddKeywords(bg, bot, 1, []string{"отель", "ОТЕЛЬ"}); err != nil || store.calls["SetCategoryKeywords"] != 0 || len(index.calls) != 0 {
		t.Fatalf("nothing new must write nothing: %v %v", err, index.calls)
	}

	if err := svc.AddKeywords(bg, Actor{Mode: ModeInternal}, 1, []string{"Хостел", "хостел", "отель", "Апартаменты"}); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(store.categories[1].Keywords); got != "[Отель хостел апартаменты]" {
		t.Fatalf("stored %s", got)
	}
	if got := fmt.Sprint(index.added[1]); got != "[хостел апартаменты]" {
		t.Fatalf("indexed %s", got)
	}

	index.keywordsErr = errors.New("es down")
	if err := svc.AddKeywords(bg, bot, 1, []string{"гостиница"}); err != nil {
		t.Fatalf("index failure must be ignored: %v", err)
	}

	store.failSetKeywords = &IntegrityError{SQLState: "23514", Message: "check"}
	wantErr(t, svc.AddKeywords(bg, bot, 1, []string{"мотель"}), KindValidation, "Не удалось сохранить категорию: такое название или ключевое слово уже есть")
}

func TestDictionarySeed(t *testing.T) {
	store, index, svc := newDictionaryFixture()
	store.categories[1] = Category{ID: 1, Name: "Old name", Keywords: []string{"мой"}}
	_, _ = svc.Currencies(bg)

	data := SeedData{
		Currencies: []Currency{{Code: "EUR"}},
		Countries:  []Country{{Code: "DE"}},
		Categories: []Category{{ID: 1, Name: "Жильё", Keywords: []string{"отель"}}, {ID: 2, Name: "Транспорт", Keywords: []string{"такси"}}},
	}
	if err := svc.Seed(bg, data); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(index.calls) != "[Ensure SyncAll]" {
		t.Fatalf("index calls %v", index.calls)
	}

	if index.synced[0].Name != "Жильё" || fmt.Sprint(index.synced[0].Keywords) != "[мой]" || index.synced[1].ID != 2 {
		t.Fatalf("synced %+v", index.synced)
	}
	currencies, _ := svc.Currencies(bg)
	if len(currencies.Result) != 1 || currencies.Result[0].Code != "EUR" {
		t.Fatalf("seed must reset the dictionary cache: %+v", currencies)
	}

	index.calls, index.ensureErr = nil, errors.New("es down")
	if err := svc.Seed(bg, data); err != nil || fmt.Sprint(index.calls) != "[Ensure]" {
		t.Fatalf("ensure failure: %v %v", err, index.calls)
	}
	index.calls, index.ensureErr, index.syncErr = nil, nil, errors.New("bulk failed")
	if err := svc.Seed(bg, data); err != nil {
		t.Fatalf("sync failure: %v", err)
	}

	store.failSeed = errors.New("db down")
	index.calls = nil
	if err := svc.Seed(bg, data); !errors.Is(err, store.failSeed) || len(index.calls) != 0 {
		t.Fatalf("store failure: %v %v", err, index.calls)
	}
}
