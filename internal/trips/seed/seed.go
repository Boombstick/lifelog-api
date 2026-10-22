package seed

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"

	"lifelog-api/internal/trips"
)

var (
	//go:embed currencies.json
	currenciesJSON []byte
	//go:embed countries.json
	countriesJSON []byte
	//go:embed expense_categories.json
	categoriesJSON []byte
)

func Load() (trips.SeedData, error) {
	var data trips.SeedData
	if err := decode("currencies.json", currenciesJSON, &data.Currencies); err != nil {
		return data, err
	}
	if err := decode("countries.json", countriesJSON, &data.Countries); err != nil {
		return data, err
	}
	if err := decode("expense_categories.json", categoriesJSON, &data.Categories); err != nil {
		return data, err
	}
	return data, nil
}

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

func decode(name string, raw []byte, into any) error {
	if err := json.Unmarshal(bytes.TrimPrefix(raw, utf8BOM), into); err != nil {
		return fmt.Errorf("seed: %s: %w", name, err)
	}
	return nil
}
