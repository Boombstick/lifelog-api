package indexer

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"lifelog-api/internal/film"
	"lifelog-api/internal/search"
)

const (
	oldFilms  = "test_films_20260101t000000"
	oldPeople = "test_people_20260101t000000"
)

type fakeCluster struct {
	mu sync.Mutex

	legacy bool

	holders map[string]string

	bulkStatus int
	failItem   string

	calls        []string
	created      []string
	bulkTargets  []string
	aliasActions []map[string]map[string]string
	deleted      []string
}

func (f *fakeCluster) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	w.Header().Set("X-Elastic-Product", "Elasticsearch")
	w.Header().Set("Content-Type", "application/json")
	path := r.URL.Path
	f.calls = append(f.calls, r.Method+" "+path)

	switch {
	case r.Method == http.MethodHead:
		name := strings.TrimPrefix(path, "/")
		if f.legacy && (name == "test_films" || name == "test_people") {
			return
		}
		if _, ok := f.holders[name]; ok {
			return
		}
		w.WriteHeader(http.StatusNotFound)

	case r.Method == http.MethodPut:
		f.created = append(f.created, strings.TrimPrefix(path, "/"))
		fmt.Fprint(w, `{"acknowledged":true}`)

	case r.Method == http.MethodGet && strings.HasPrefix(path, "/_alias/"):
		alias := strings.TrimPrefix(path, "/_alias/")
		index, ok := f.holders[alias]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprintf(w, `{"error":"alias [%s] missing","status":404}`, alias)
			return
		}
		fmt.Fprintf(w, `{%q:{"aliases":{%q:{}}}}`, index, alias)

	case r.Method == http.MethodGet && strings.HasPrefix(path, "/_cat/indices/"):
		pattern := strings.TrimSuffix(strings.TrimPrefix(path, "/_cat/indices/"), "*")
		names := slices.Clone(f.created)
		if f.holders != nil {
			names = append(names, oldFilms, oldPeople)
		}

		names = append(names, pattern+"backup")
		rows := []string{}
		for _, name := range names {
			if strings.HasPrefix(name, pattern) {
				rows = append(rows, fmt.Sprintf(`{"index":%q}`, name))
			}
		}
		fmt.Fprintf(w, "[%s]", strings.Join(rows, ","))

	case r.Method == http.MethodPost && path == "/_bulk":
		f.recordBulk(w, r.Body)

	case r.Method == http.MethodPost && strings.HasSuffix(path, "/_refresh"):
		fmt.Fprint(w, `{"_shards":{"total":1,"successful":1,"failed":0}}`)

	case r.Method == http.MethodPost && path == "/_aliases":
		var body struct {
			Actions []map[string]map[string]string `json:"actions"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.aliasActions = body.Actions
		fmt.Fprint(w, `{"acknowledged":true}`)

	case r.Method == http.MethodDelete:
		f.deleted = append(f.deleted, strings.Split(strings.TrimPrefix(path, "/"), ",")...)
		fmt.Fprint(w, `{"acknowledged":true}`)

	default:
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"unexpected call"}`)
	}
}

func (f *fakeCluster) recordBulk(w http.ResponseWriter, body io.Reader) {
	if f.bulkStatus != 0 && f.bulkStatus != http.StatusOK {
		w.WriteHeader(f.bulkStatus)
		fmt.Fprint(w, `{"error":{"type":"boom","reason":"cluster broke"}}`)
		return
	}
	var items []string
	failed := false
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for line := 0; scanner.Scan(); line++ {
		if line%2 == 1 {
			continue
		}
		var meta struct {
			Index struct {
				Index string `json:"_index"`
				ID    string `json:"_id"`
			} `json:"index"`
		}
		_ = json.Unmarshal(scanner.Bytes(), &meta)
		f.bulkTargets = append(f.bulkTargets, meta.Index.Index)
		if meta.Index.ID == f.failItem {
			failed = true
			items = append(items, fmt.Sprintf(`{"index":{"_id":%q,"status":400,"error":{"type":"mapper_parsing_exception","reason":"%s"}}}`,
				meta.Index.ID, strings.Repeat("x", 500)))
			continue
		}
		items = append(items, fmt.Sprintf(`{"index":{"_id":%q,"status":201}}`, meta.Index.ID))
	}
	fmt.Fprintf(w, `{"errors":%t,"items":[%s]}`, failed, strings.Join(items, ","))
}

type fakeCatalog struct{ films int }

func (c fakeCatalog) FilmsBatch(_ context.Context, afterID uint, limit int) ([]film.Film, error) {
	var out []film.Film
	for id := afterID + 1; id <= uint(c.films) && len(out) < limit; id++ {
		out = append(out, film.Film{ID: id, TitleRu: "Фильм", Kind: film.KindMovie})
	}
	return out, nil
}

func (c fakeCatalog) PeopleBatch(_ context.Context, afterID uint, _ int) ([]PersonRow, error) {
	if afterID > 0 {
		return nil, nil
	}
	return []PersonRow{{ID: 1, Name: "Дени Вильнёв"}}, nil
}

func runIndexer(t *testing.T, cluster *fakeCluster, opts Options) (Stats, error) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(cluster.serve))
	t.Cleanup(server.Close)

	client, err := search.NewClient(search.Options{Addresses: []string{server.URL}, IndexPrefix: "test"})
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(fakeCatalog{films: 3}, client, logger).Run(context.Background(), opts)
}

func newGeneration(t *testing.T, cluster *fakeCluster) (films, people string) {
	t.Helper()
	if len(cluster.created) != 2 {
		t.Fatalf("created %v, want a films and a people index", cluster.created)
	}
	films, people = cluster.created[0], cluster.created[1]
	if !strings.HasPrefix(films, "test_films_") || !strings.HasPrefix(people, "test_people_") {
		t.Fatalf("created %v, want versioned names", cluster.created)
	}
	return films, people
}

func TestRecreateFillsNewGenerationThenSwapsAliases(t *testing.T) {
	t.Parallel()

	cluster := &fakeCluster{holders: map[string]string{"test_films": oldFilms, "test_people": oldPeople}}
	stats, err := runIndexer(t, cluster, Options{Recreate: true, Batch: 2})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if stats.Films != 3 || stats.People != 1 {
		t.Errorf("stats %+v", stats)
	}

	films, people := newGeneration(t, cluster)
	for _, target := range cluster.bulkTargets {
		if target != films && target != people {
			t.Errorf("bulk wrote into %q, not the new generation", target)
		}
	}

	want := []map[string]map[string]string{
		{"remove": {"index": oldFilms, "alias": "test_films"}},
		{"add": {"index": films, "alias": "test_films"}},
		{"remove": {"index": oldPeople, "alias": "test_people"}},
		{"add": {"index": people, "alias": "test_people"}},
	}
	if fmt.Sprint(cluster.aliasActions) != fmt.Sprint(want) {
		t.Errorf("alias actions\n got %v\nwant %v", cluster.aliasActions, want)
	}

	slices.Sort(cluster.deleted)
	if fmt.Sprint(cluster.deleted) != fmt.Sprint([]string{oldFilms, oldPeople}) {
		t.Errorf("deleted %v, want only the old generation", cluster.deleted)
	}

	refresh := slices.Index(cluster.calls, "POST /"+films+","+people+"/_refresh")
	swap := slices.Index(cluster.calls, "POST /_aliases")
	if refresh < 0 || swap < refresh {
		t.Errorf("calls out of order: %v", cluster.calls)
	}
}

func TestRecreateReplacesLegacyIndexInTheSameCall(t *testing.T) {
	t.Parallel()

	cluster := &fakeCluster{legacy: true}
	if _, err := runIndexer(t, cluster, Options{Recreate: true}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	films, people := newGeneration(t, cluster)
	want := []map[string]map[string]string{
		{"remove_index": {"index": "test_films"}},
		{"add": {"index": films, "alias": "test_films"}},
		{"remove_index": {"index": "test_people"}},
		{"add": {"index": people, "alias": "test_people"}},
	}
	if fmt.Sprint(cluster.aliasActions) != fmt.Sprint(want) {
		t.Errorf("alias actions\n got %v\nwant %v", cluster.aliasActions, want)
	}
	if len(cluster.deleted) != 0 {
		t.Errorf("deleted %v; the legacy index goes inside _aliases", cluster.deleted)
	}
}

func TestRecreateFailureDropsNewGenerationAndKeepsAliases(t *testing.T) {
	t.Parallel()

	cluster := &fakeCluster{
		holders:    map[string]string{"test_films": oldFilms, "test_people": oldPeople},
		bulkStatus: http.StatusInternalServerError,
	}
	if _, err := runIndexer(t, cluster, Options{Recreate: true}); err == nil {
		t.Fatal("a failed bulk must fail the run")
	}
	films, people := newGeneration(t, cluster)
	if cluster.aliasActions != nil {
		t.Errorf("aliases touched: %v", cluster.aliasActions)
	}
	slices.Sort(cluster.deleted)
	if fmt.Sprint(cluster.deleted) != fmt.Sprint([]string{films, people}) {
		t.Errorf("deleted %v, want the new generation", cluster.deleted)
	}
}

func TestRefusedDocumentDoesNotStopThePass(t *testing.T) {
	t.Parallel()

	cluster := &fakeCluster{
		holders:  map[string]string{"test_films": oldFilms, "test_people": oldPeople},
		failItem: "1",
	}
	stats, err := runIndexer(t, cluster, Options{Recreate: true, Batch: 1})
	if !errors.Is(err, ErrDocumentsFailed) {
		t.Fatalf("err = %v, want ErrDocumentsFailed", err)
	}

	if stats.Failed != 2 || stats.Films != 2 || stats.People != 0 {
		t.Errorf("stats %+v", stats)
	}
	if bulks := len(cluster.bulkTargets); bulks != 4 {
		t.Errorf("%d documents sent, want all 4 despite the refusals", bulks)
	}
	if cluster.aliasActions != nil {
		t.Errorf("aliases switched to an incomplete generation: %v", cluster.aliasActions)
	}
}

func TestPlainRunWritesThroughAliases(t *testing.T) {
	t.Parallel()

	cluster := &fakeCluster{holders: map[string]string{"test_films": oldFilms, "test_people": oldPeople}}
	if _, err := runIndexer(t, cluster, Options{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(cluster.created) != 0 {
		t.Errorf("created %v over existing aliases", cluster.created)
	}
	for _, target := range cluster.bulkTargets {
		if target != "test_films" && target != "test_people" {
			t.Errorf("bulk wrote into %q, not the alias", target)
		}
	}
}

func TestTruncateKeepsRunes(t *testing.T) {
	t.Parallel()

	got := truncate(strings.Repeat("ж", 10), 5)
	if got != "жж…" {
		t.Errorf("got %q", got)
	}
	if truncate("short", 10) != "short" {
		t.Error("short text changed")
	}
}
