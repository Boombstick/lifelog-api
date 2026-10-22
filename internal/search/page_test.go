package search

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func cannedCluster(t *testing.T, total, n int) (*Client, *string) {
	t.Helper()

	var lastBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		lastBody = string(body)

		hits := make([]string, 0, n)
		for i := range n {
			hits = append(hits, fmt.Sprintf(`{"_source":{"id":%d,"title_sort":"Фильм %d"},"sort":[%d,%d]}`, i+1, i+1, 100-i, i+1))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Elastic-Product", "Elasticsearch")
		fmt.Fprintf(w, `{"hits":{"total":{"value":%d},"hits":[%s]}}`, total, strings.Join(hits, ","))
	}))
	t.Cleanup(server.Close)

	client, err := NewClient(Options{Addresses: []string{server.URL}, IndexPrefix: "test"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client, &lastBody
}

func TestNextCursorOnlyOnAFullPage(t *testing.T) {
	t.Parallel()

	t.Run("full page offers a cursor", func(t *testing.T) {
		client, _ := cannedCluster(t, 57, 20)
		page, err := client.SearchFilms(context.Background(), Query{Limit: 20})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 20 || page.Total != 57 {
			t.Fatalf("items=%d total=%d", len(page.Items), page.Total)
		}
		if len(page.NextAfter) != 2 {
			t.Fatalf("a full page must carry the last hit's sort values, got %v", page.NextAfter)
		}

		if fmt.Sprint(page.NextAfter) != "[81 20]" {
			t.Errorf("cursor = %v, want the last hit's sort values", page.NextAfter)
		}
	})

	t.Run("short page ends the list", func(t *testing.T) {
		client, _ := cannedCluster(t, 7, 7)
		page, err := client.SearchFilms(context.Background(), Query{Limit: 20})
		if err != nil {
			t.Fatal(err)
		}
		if page.NextAfter != nil {
			t.Errorf("a short page must end the list, got cursor %v", page.NextAfter)
		}
	})

	t.Run("empty page ends the list", func(t *testing.T) {
		client, _ := cannedCluster(t, 0, 0)
		page, err := client.SearchFilms(context.Background(), Query{Limit: 20})
		if err != nil {
			t.Fatal(err)
		}
		if page.NextAfter != nil || len(page.Items) != 0 {
			t.Errorf("empty page: items=%d cursor=%v", len(page.Items), page.NextAfter)
		}
	})
}

func TestSearchBodyCarriesTotalsAndCursor(t *testing.T) {
	t.Parallel()

	client, body := cannedCluster(t, 3, 3)
	if _, err := client.SearchFilms(context.Background(), Query{Limit: 20, After: []any{81, 20}}); err != nil {
		t.Fatal(err)
	}

	var sent map[string]any
	if err := json.Unmarshal([]byte(*body), &sent); err != nil {
		t.Fatalf("request body is not JSON: %v (%s)", err, *body)
	}
	if sent["track_total_hits"] != true {
		t.Error("without track_total_hits the count stops at 10 000 and the interface lies")
	}
	after, ok := sent["search_after"].([]any)
	if !ok || len(after) != 2 {
		t.Errorf("search_after = %v, want the client's cursor", sent["search_after"])
	}
	if sent["size"] != float64(20) {
		t.Errorf("size = %v, want 20", sent["size"])
	}
}
