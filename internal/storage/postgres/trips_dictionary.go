package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"lifelog-api/internal/trips"
)

const (
	tripsCountryCols  = `code, name, name_ru, default_currency_code, is_multi_currency, default_timezone, is_multi_timezone`
	tripsCurrencyCols = `code, name, name_ru, symbol`

	tripsCategoryCols = `id, name, to_jsonb(keywords)::text`
)

func scanTripsCountry(row interface{ Scan(...any) error }) (trips.Country, error) {
	var c trips.Country
	err := row.Scan(&c.Code, &c.Name, &c.NameRu, &c.DefaultCurrencyCode, &c.IsMultiCurrency, &c.DefaultTimezone, &c.IsMultiTimezone)
	return c, err
}

func scanTripsCurrency(row interface{ Scan(...any) error }) (trips.Currency, error) {
	var c trips.Currency
	err := row.Scan(&c.Code, &c.Name, &c.NameRu, &c.Symbol)
	return c, err
}

func scanTripsCategory(row interface{ Scan(...any) error }) (trips.Category, error) {
	var (
		c        trips.Category
		keywords string
	)
	if err := row.Scan(&c.ID, &c.Name, &keywords); err != nil {
		return c, err
	}
	if err := json.Unmarshal([]byte(keywords), &c.Keywords); err != nil {
		return c, err
	}
	if c.Keywords == nil {
		c.Keywords = []string{}
	}
	return c, nil
}

func tripsQueryAll[T any](ctx context.Context, s *TripsStore, scan func(interface{ Scan(...any) error }) (T, error), query string, args ...any) ([]T, error) {
	q, err := s.querier()
	if err != nil {
		return nil, err
	}
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *TripsStore) Countries(ctx context.Context) ([]trips.Country, error) {
	return tripsQueryAll(ctx, s, scanTripsCountry, `SELECT `+tripsCountryCols+` FROM countries ORDER BY name_ru, code`)
}

func (s *TripsStore) Currencies(ctx context.Context) ([]trips.Currency, error) {
	return tripsQueryAll(ctx, s, scanTripsCurrency, `SELECT `+tripsCurrencyCols+` FROM currencies ORDER BY code COLLATE "C"`)
}

func (s *TripsStore) CountryByName(ctx context.Context, name string) (*trips.Country, error) {
	found, err := tripsQueryAll(ctx, s, scanTripsCountry,
		`SELECT `+tripsCountryCols+` FROM countries WHERE lower(name) = $1 OR lower(name_ru) = $1 LIMIT 2`, name)
	if err != nil {
		return nil, err
	}
	switch len(found) {
	case 0:
		return nil, nil
	case 1:
		return &found[0], nil
	}
	return nil, trips.Conflict(trips.MsgSequenceMoreThanOne)
}

func (s *TripsStore) CurrencyByCode(ctx context.Context, code string) (*trips.Currency, error) {
	q, err := s.querier()
	if err != nil {
		return nil, err
	}
	c, err := scanTripsCurrency(q.QueryRowContext(ctx, `SELECT `+tripsCurrencyCols+` FROM currencies WHERE code = $1`, code))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *TripsStore) Categories(ctx context.Context) ([]trips.Category, error) {
	return tripsQueryAll(ctx, s, scanTripsCategory, `SELECT `+tripsCategoryCols+` FROM expense_categories ORDER BY id`)
}

func (s *TripsStore) CategoryByID(ctx context.Context, id int64) (*trips.Category, error) {
	q, err := s.querier()
	if err != nil {
		return nil, err
	}
	c, err := scanTripsCategory(q.QueryRowContext(ctx, `SELECT `+tripsCategoryCols+` FROM expense_categories WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func tripsKeywords(k []string) []string {
	if k == nil {
		return []string{}
	}
	return k
}

func (s *TripsStore) InsertCategory(ctx context.Context, category *trips.Category) error {
	if err := tripsCheckLengths(tripsLengthCheck{"name", &category.Name, trips.MaxCategoryNameLen}); err != nil {
		return err
	}
	return s.guarded(ctx, func() error {
		err := s.q.QueryRowContext(ctx,
			`INSERT INTO expense_categories (name, keywords) VALUES ($1, $2) RETURNING id`,
			category.Name, tripsKeywords(category.Keywords)).Scan(&category.ID)
		return tripsMapErr(err, "")
	})
}

func (s *TripsStore) SetCategoryKeywords(ctx context.Context, id int64, keywords []string) error {
	return s.guarded(ctx, func() error {
		_, err := s.q.ExecContext(ctx, `UPDATE expense_categories SET keywords = $2 WHERE id = $1`, id, tripsKeywords(keywords))
		return tripsMapErr(err, "")
	})
}

func (s *TripsStore) SeedDictionaries(ctx context.Context, data trips.SeedData) error {
	return s.InTx(ctx, func(txStore trips.Store) error {
		tx := txStore.(*TripsStore).q

		for _, c := range data.Categories {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO expense_categories (id, name, keywords) VALUES ($1, $2, $3)
				 ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name`,
				c.ID, c.Name, tripsKeywords(c.Keywords)); err != nil {
				return tripsMapErr(err, "")
			}
		}
		for _, c := range data.Currencies {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO currencies (code, name, name_ru, symbol) VALUES ($1, $2, $3, $4)
				 ON CONFLICT (code) DO UPDATE SET name = EXCLUDED.name, name_ru = EXCLUDED.name_ru, symbol = EXCLUDED.symbol`,
				c.Code, c.Name, c.NameRu, c.Symbol); err != nil {
				return tripsMapErr(err, "")
			}
		}
		for _, c := range data.Countries {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO countries (code, name, name_ru, default_currency_code, is_multi_currency, default_timezone, is_multi_timezone)
				 VALUES ($1, $2, $3, $4, $5, $6, $7)
				 ON CONFLICT (code) DO UPDATE SET name = EXCLUDED.name, name_ru = EXCLUDED.name_ru,
				   default_currency_code = EXCLUDED.default_currency_code, is_multi_currency = EXCLUDED.is_multi_currency,
				   default_timezone = EXCLUDED.default_timezone, is_multi_timezone = EXCLUDED.is_multi_timezone`,
				c.Code, c.Name, c.NameRu, c.DefaultCurrencyCode, c.IsMultiCurrency, c.DefaultTimezone, c.IsMultiTimezone); err != nil {
				return tripsMapErr(err, "")
			}
		}
		_, err := tx.ExecContext(ctx,
			`SELECT setval(pg_get_serial_sequence('expense_categories', 'id'),
			        GREATEST((SELECT COALESCE(MAX(id), 1) FROM expense_categories), 1))`)
		return err
	})
}
