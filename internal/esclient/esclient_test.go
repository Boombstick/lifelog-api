package esclient

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIndexHealthResolvesAliases(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Elastic-Product", "Elasticsearch")
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/_cat/indices":
			fmt.Fprint(w, `[{"index":"x_films_20261019t203500","health":"green"},
				{"index":"x_people_20261019t203500","health":"yellow"},
				{"index":"trips_categories","health":"green"}]`)
		case "/_cat/aliases":
			fmt.Fprint(w, `[{"alias":"x_films","index":"x_films_20261019t203500"},
				{"alias":"x_people","index":"x_people_20261019t203500"}]`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	es, err := New(Options{Addresses: []string{server.URL}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := IndexHealth(context.Background(), es)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{
		"x_films":          "green",
		"x_people":         "yellow",
		"trips_categories": "green",
	} {
		if got[name] != want {
			t.Errorf("%s = %q, want %q (all: %v)", name, got[name], want, got)
		}
	}
}
