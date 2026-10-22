package seed

import "testing"

func TestLoad(t *testing.T) {
	data, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Currencies) != 166 || len(data.Countries) != 250 || len(data.Categories) != 10 {
		t.Fatalf("counts: %d currencies, %d countries, %d categories",
			len(data.Currencies), len(data.Countries), len(data.Categories))
	}

	usd := data.Currencies[0]
	if usd.Code != "USD" || usd.NameRu != "Доллар США" || usd.Symbol != "$" {
		t.Errorf("PascalCase keys not decoded: %+v", usd)
	}

	multiTZ := 0
	for _, c := range data.Countries {
		if c.DefaultTimezone == "" || c.DefaultCurrencyCode == "" || c.NameRu == "" {
			t.Errorf("country with blank fields: %+v", c)
		}
		if c.IsMultiTimezone {
			multiTZ++
		}
	}
	if multiTZ != 25 {
		t.Errorf("multi-timezone countries = %d, want 25", multiTZ)
	}

	for i, c := range data.Categories {
		if c.ID != int64(i+1) || c.Name == "" || c.Keywords == nil || (c.ID != 10 && len(c.Keywords) == 0) {
			t.Errorf("category %d: %+v", i, c)
		}
	}
	if last := data.Categories[9]; last.Name != "Прочее" {
		t.Errorf("category 10 = %q", last.Name)
	}
}
