package category

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"lifelog-api/internal/trips"

	"github.com/elastic/go-elasticsearch/v8/esapi"
)

func TestIntegrationLocalES(t *testing.T) {
	url := os.Getenv("TRIPS_CATEGORY_ES_URL")
	if url == "" {
		t.Skip("TRIPS_CATEGORY_ES_URL is not set")
	}
	name := fmt.Sprintf("test_trips_categories_%d", time.Now().UnixNano())
	if name == DefaultIndex {
		t.Fatal("refusing to use the live index")
	}

	index, err := New(Options{
		Addresses: strings.Split(url, ","),
		Username:  os.Getenv("TRIPS_CATEGORY_ES_USERNAME"),
		Password:  os.Getenv("TRIPS_CATEGORY_ES_PASSWORD"),
		Index:     name,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	t.Cleanup(func() {
		res, err := esapi.IndicesDeleteRequest{Index: []string{name}}.Do(context.Background(), index.es)
		if err != nil {
			t.Errorf("delete test index: %v", err)
			return
		}
		drain(res)
	})

	for range 2 {
		if err := index.Ensure(ctx); err != nil {
			t.Fatal(err)
		}
	}

	seed := []trips.Category{
		{ID: 1, Name: "Жильё", Keywords: []string{"отель", "хостел", "аренда"}},
		{ID: 3, Name: "Кафе и рестораны", Keywords: []string{"кофе", "ресторан", "обед"}},
		{ID: 10, Name: "Прочее", Keywords: nil},
	}
	if err := index.SyncAll(ctx, seed); err != nil {
		t.Fatal(err)
	}

	if err := index.SyncAll(ctx, seed); err != nil {
		t.Fatal(err)
	}
	refresh(t, index, name)

	cases := []struct {
		description string
		want        int64
	}{
		{"кофе", 3},
		{"Отели", 1},
		{"хастел", 1},
	}
	for _, c := range cases {
		got, err := index.Search(ctx, c.description)
		if err != nil {
			t.Fatal(err)
		}
		if got == nil || got.ID != c.want {
			t.Errorf("Search(%q) = %+v, want id %d", c.description, got, c.want)
		}
	}
	if got, err := index.Search(ctx, "qwertyuiop"); err != nil || got != nil {
		t.Errorf("Search(nonsense) = %+v, %v", got, err)
	}

	if err := index.Put(ctx, trips.Category{ID: 11, Name: "Спорт"}); err != nil {
		t.Fatal(err)
	}
	if err := index.AddKeywords(ctx, 11, []string{"бассейн"}); err != nil {
		t.Fatal(err)
	}
	refresh(t, index, name)
	if got, err := index.Search(ctx, "бассейн"); err != nil || got == nil || got.ID != 11 || got.Name != "Спорт" {
		t.Errorf("Search(бассейн) = %+v, %v", got, err)
	}

	if err := index.AddKeywords(ctx, 999, []string{"x"}); err == nil {
		t.Error("AddKeywords on a missing document: want an error")
	}
}

func refresh(t *testing.T, index *Index, name string) {
	t.Helper()
	res, err := esapi.IndicesRefreshRequest{Index: []string{name}}.Do(context.Background(), index.es)
	if err != nil {
		t.Fatal(err)
	}
	defer drain(res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("refresh: %s", res.Status())
	}
}
