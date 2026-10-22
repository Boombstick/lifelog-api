package category

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"lifelog-api/internal/trips"
)

type recorded struct {
	Method string
	Path   string
	Query  string
	Body   string
}

type fakeES struct {
	t        *testing.T
	mu       sync.Mutex
	requests []recorded

	handle func(r recorded) (int, string)
}

func newFake(t *testing.T, handle func(r recorded) (int, string)) (*Index, *fakeES) {
	t.Helper()
	fake := &fakeES{t: t, handle: handle}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rec := recorded{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Body: string(body)}
		fake.mu.Lock()
		fake.requests = append(fake.requests, rec)
		fake.mu.Unlock()

		status, out := fake.handle(rec)

		w.Header().Set("X-Elastic-Product", "Elasticsearch")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, out)
	}))
	t.Cleanup(server.Close)

	index, err := New(Options{Addresses: []string{server.URL}, Index: "test_categories"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return index, fake
}

func (f *fakeES) only() recorded {
	f.t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) != 1 {
		f.t.Fatalf("want 1 request, got %d: %+v", len(f.requests), f.requests)
	}
	return f.requests[0]
}

func assertJSON(t *testing.T, got, want string) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal([]byte(got), &g); err != nil {
		t.Fatalf("body is not JSON: %v\n%s", err, got)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("want is not JSON: %v", err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Fatalf("body mismatch\n got: %s\nwant: %s", got, want)
	}
}

func TestNewRequiresAddress(t *testing.T) {
	if _, err := New(Options{Addresses: []string{"", " "}}, nil); err == nil {
		t.Fatal("want an error without an address")
	}
}

func TestNewDefaultIndex(t *testing.T) {
	index, err := New(Options{Addresses: []string{"http://localhost:9200"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if index.index != "categories" {
		t.Fatalf("index = %q", index.index)
	}
}

func TestSearchRequestMatchesCSharp(t *testing.T) {
	index, fake := newFake(t, func(recorded) (int, string) {
		return 200, `{"hits":{"total":{"value":1},"hits":[{"_id":"3","_score":12.5,"_source":{"id":3,"name":"Кафе и рестораны","keywords":["кофе"]}}]}}`
	})

	got, err := index.Search(context.Background(), "кофе с собой")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.ID != 3 || got.Name != "Кафе и рестораны" {
		t.Fatalf("got %+v", got)
	}

	req := fake.only()
	if req.Method != http.MethodPost || req.Path != "/test_categories/_search" {
		t.Fatalf("request %s %s", req.Method, req.Path)
	}
	assertJSON(t, req.Body, `{"size":1,"query":{"bool":{"should":[
		{"match":{"keywords":{"query":"кофе с собой","boost":10}}},
		{"match":{"name":{"query":"кофе с собой","boost":1}}},
		{"fuzzy":{"keywords":{"value":"кофе с собой","fuzziness":"AUTO","boost":5}}}
	],"minimum_should_match":1}}}`)
}

func TestSearchNoHitIsNil(t *testing.T) {
	index, _ := newFake(t, func(recorded) (int, string) {
		return 200, `{"hits":{"total":{"value":0},"hits":[]}}`
	})
	got, err := index.Search(context.Background(), "xyz")
	if err != nil || got != nil {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestSearchErrorResponse(t *testing.T) {
	index, _ := newFake(t, func(recorded) (int, string) {
		return 404, `{"error":{"type":"index_not_found_exception","reason":"no such index [test_categories]"},"status":404}`
	})
	got, err := index.Search(context.Background(), "кофе")
	if got != nil {
		t.Fatalf("got %+v", got)
	}
	var esErr *esError
	if !errors.As(err, &esErr) || esErr.Status != 404 || esErr.Type != "index_not_found_exception" {
		t.Fatalf("err = %v", err)
	}
}

func TestSearchTransportError(t *testing.T) {
	index, err := New(Options{Addresses: []string{"http://127.0.0.1:1"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := index.Search(context.Background(), "кофе"); err == nil || got != nil {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestEnsureExistingIndexDoesNothing(t *testing.T) {
	index, fake := newFake(t, func(recorded) (int, string) { return 200, `` })
	if err := index.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	req := fake.only()
	if req.Method != http.MethodHead || req.Path != "/test_categories" {
		t.Fatalf("request %s %s", req.Method, req.Path)
	}
}

func TestEnsureCreatesMissingIndex(t *testing.T) {
	index, fake := newFake(t, func(r recorded) (int, string) {
		if r.Method == http.MethodHead {
			return 404, ``
		}
		return 200, `{"acknowledged":true,"shards_acknowledged":true,"index":"test_categories"}`
	})
	if err := index.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fake.requests) != 2 {
		t.Fatalf("requests: %+v", fake.requests)
	}
	create := fake.requests[1]
	if create.Method != http.MethodPut || create.Path != "/test_categories" {
		t.Fatalf("request %s %s", create.Method, create.Path)
	}
	assertJSON(t, create.Body, `{"settings":{"number_of_replicas":0,"analysis":{
		"filter":{"russian_stemmer":{"type":"stemmer","language":"russian"},"russian_stop":{"type":"stop","stopwords":["и","в","на","с","у"]}},
		"analyzer":{"russian_analyzer":{"type":"custom","tokenizer":"standard","filter":["lowercase","russian_stemmer","russian_stop"]}}}},
		"mappings":{"properties":{"id":{"type":"integer"},"name":{"type":"text","analyzer":"russian_analyzer"},"keywords":{"type":"text","analyzer":"russian_analyzer"}}}}`)
}

func TestEnsureRaceAlreadyExists(t *testing.T) {
	index, _ := newFake(t, func(r recorded) (int, string) {
		if r.Method == http.MethodHead {
			return 404, ``
		}
		return 400, `{"error":{"type":"resource_already_exists_exception","reason":"index [test_categories/abc] already exists"},"status":400}`
	})
	if err := index.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureCreateFails(t *testing.T) {
	index, _ := newFake(t, func(r recorded) (int, string) {
		if r.Method == http.MethodHead {
			return 404, ``
		}
		return 400, `{"error":{"type":"illegal_argument_exception","reason":"bad"},"status":400}`
	})
	if err := index.Ensure(context.Background()); err == nil || !strings.Contains(err.Error(), "illegal_argument_exception") {
		t.Fatalf("err = %v", err)
	}
}

func TestEnsureUnexpectedStatus(t *testing.T) {
	index, _ := newFake(t, func(recorded) (int, string) { return 503, `` })
	if err := index.Ensure(context.Background()); err == nil {
		t.Fatal("want an error")
	}
}

var twoCategories = []trips.Category{
	{ID: 1, Name: "Жильё", Keywords: []string{"отель", "хостел"}},
	{ID: 11, Name: "Новая", Keywords: nil},
}

func TestSyncAllBulkBody(t *testing.T) {
	index, fake := newFake(t, func(recorded) (int, string) {
		return 200, `{"took":3,"errors":false,"items":[{"index":{"_id":"1","status":200}},{"index":{"_id":"11","status":201}}]}`
	})
	if err := index.SyncAll(context.Background(), twoCategories); err != nil {
		t.Fatal(err)
	}

	req := fake.only()
	if req.Method != http.MethodPost || req.Path != "/test_categories/_bulk" {
		t.Fatalf("request %s %s", req.Method, req.Path)
	}
	lines := strings.Split(strings.TrimRight(req.Body, "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("bulk lines: %q", lines)
	}
	assertJSON(t, lines[0], `{"index":{"_index":"test_categories","_id":"1"}}`)
	assertJSON(t, lines[1], `{"id":1,"name":"Жильё","keywords":["отель","хостел"]}`)
	assertJSON(t, lines[2], `{"index":{"_index":"test_categories","_id":"11"}}`)
	assertJSON(t, lines[3], `{"id":11,"name":"Новая","keywords":[]}`)
}

func TestSyncAllPartialFailure(t *testing.T) {
	index, _ := newFake(t, func(recorded) (int, string) {
		return 200, `{"took":3,"errors":true,"items":[
			{"index":{"_id":"1","status":200}},
			{"index":{"_id":"11","status":400,"error":{"type":"mapper_parsing_exception","reason":"failed to parse field [id]"}}}]}`
	})
	err := index.SyncAll(context.Background(), twoCategories)
	if err == nil {
		t.Fatal("want an error on a rejected document")
	}
	for _, part := range []string{"1 of 2", "id 11", "mapper_parsing_exception"} {
		if !strings.Contains(err.Error(), part) {
			t.Fatalf("err %q lacks %q", err, part)
		}
	}
}

func TestSyncAllErrorsFlagWithoutItem(t *testing.T) {
	index, _ := newFake(t, func(recorded) (int, string) {
		return 200, `{"errors":true,"items":[]}`
	})
	if err := index.SyncAll(context.Background(), twoCategories); err == nil {
		t.Fatal("want an error")
	}
}

func TestSyncAllRequestError(t *testing.T) {
	index, _ := newFake(t, func(recorded) (int, string) {
		return 500, `{"error":{"type":"x","reason":"y"},"status":500}`
	})
	if err := index.SyncAll(context.Background(), twoCategories); err == nil {
		t.Fatal("want an error")
	}
}

func TestSyncAllEmptySendsNothing(t *testing.T) {
	index, fake := newFake(t, func(recorded) (int, string) { return 200, `{}` })
	if err := index.SyncAll(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if len(fake.requests) != 0 {
		t.Fatalf("requests: %+v", fake.requests)
	}
}

func TestPut(t *testing.T) {
	index, fake := newFake(t, func(recorded) (int, string) {
		return 201, `{"_id":"12","result":"created"}`
	})
	if err := index.Put(context.Background(), trips.Category{ID: 12, Name: "Спорт"}); err != nil {
		t.Fatal(err)
	}
	req := fake.only()
	if req.Method != http.MethodPut || req.Path != "/test_categories/_doc/12" || req.Query != "op_type=index" {
		t.Fatalf("request %s %s?%s", req.Method, req.Path, req.Query)
	}
	assertJSON(t, req.Body, `{"id":12,"name":"Спорт","keywords":[]}`)
}

func TestPutError(t *testing.T) {
	index, _ := newFake(t, func(recorded) (int, string) {
		return 400, `{"error":{"type":"mapper_parsing_exception","reason":"bad"},"status":400}`
	})
	if err := index.Put(context.Background(), trips.Category{ID: 12, Name: "Спорт"}); err == nil {
		t.Fatal("want an error")
	}
}

func TestAddKeywords(t *testing.T) {
	index, fake := newFake(t, func(recorded) (int, string) {
		return 200, `{"_id":"3","result":"updated"}`
	})
	if err := index.AddKeywords(context.Background(), 3, []string{"латте", "раф"}); err != nil {
		t.Fatal(err)
	}
	req := fake.only()
	if req.Method != http.MethodPost || req.Path != "/test_categories/_update/3" {
		t.Fatalf("request %s %s", req.Method, req.Path)
	}
	assertJSON(t, req.Body, `{"script":{"source":"for (k in params.keywords) {ctx._source.keywords.add(k);}","params":{"keywords":["латте","раф"]}}}`)
}

func TestAddKeywordsMissingDocument(t *testing.T) {
	index, _ := newFake(t, func(recorded) (int, string) {
		return 404, `{"error":{"type":"document_missing_exception","reason":"[3]: document missing"},"status":404}`
	})
	err := index.AddKeywords(context.Background(), 3, []string{"латте"})
	var esErr *esError
	if !errors.As(err, &esErr) || esErr.Type != "document_missing_exception" {
		t.Fatalf("err = %v", err)
	}
}

func TestAddKeywordsEmptySendsNothing(t *testing.T) {
	index, fake := newFake(t, func(recorded) (int, string) { return 200, `{}` })
	if err := index.AddKeywords(context.Background(), 3, nil); err != nil {
		t.Fatal(err)
	}
	if len(fake.requests) != 0 {
		t.Fatalf("requests: %+v", fake.requests)
	}
}
