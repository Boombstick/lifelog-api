package category

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"lifelog-api/internal/esclient"
	"lifelog-api/internal/trips"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esapi"
)

const DefaultIndex = "categories"

type Options struct {
	Addresses []string
	Username  string
	Password  string

	Index string

	Transport http.RoundTripper
}

type Index struct {
	es     *elasticsearch.Client
	index  string
	logger *slog.Logger
}

func New(opts Options, logger *slog.Logger) (*Index, error) {
	es, err := esclient.New(esclient.Options{
		Addresses: opts.Addresses,
		Username:  opts.Username,
		Password:  opts.Password,
		PoolSize:  16,
		Transport: opts.Transport,
	})
	if errors.Is(err, esclient.ErrNoAddress) {
		return nil, errors.New("category: no Elasticsearch address")
	}
	if err != nil {
		return nil, fmt.Errorf("category: Elasticsearch client: %w", err)
	}

	index := opts.Index
	if index == "" {
		index = DefaultIndex
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Index{es: es, index: index, logger: logger}, nil
}

var (
	_ trips.CategorySearch = (*Index)(nil)
	_ trips.CategoryIndex  = (*Index)(nil)
)

const createBody = `{"settings":{"number_of_replicas":0,"analysis":{"filter":{"russian_stemmer":{"type":"stemmer","language":"russian"},"russian_stop":{"type":"stop","stopwords":["и","в","на","с","у"]}},"analyzer":{"russian_analyzer":{"type":"custom","tokenizer":"standard","filter":["lowercase","russian_stemmer","russian_stop"]}}}},"mappings":{"properties":{"id":{"type":"integer"},"name":{"type":"text","analyzer":"russian_analyzer"},"keywords":{"type":"text","analyzer":"russian_analyzer"}}}}`

const addKeywordsScript = "for (k in params.keywords) {ctx._source.keywords.add(k);}"

type document struct {
	ID       int64    `json:"id"`
	Name     string   `json:"name"`
	Keywords []string `json:"keywords"`
}

func toDocument(c trips.Category) document {
	keywords := c.Keywords
	if keywords == nil {
		keywords = []string{}
	}
	return document{ID: c.ID, Name: c.Name, Keywords: keywords}
}

func (i *Index) Search(ctx context.Context, description string) (*trips.CategoryRef, error) {
	body, err := json.Marshal(searchBody(description))
	if err != nil {
		return nil, fmt.Errorf("category: search body: %w", err)
	}

	var resp struct {
		Hits struct {
			Hits []struct {
				Source *struct {
					ID   int64  `json:"id"`
					Name string `json:"name"`
				} `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	req := esapi.SearchRequest{Index: []string{i.index}, Body: bytes.NewReader(body)}
	if err := i.do(ctx, req, &resp); err != nil {
		return nil, err
	}

	if len(resp.Hits.Hits) == 0 || resp.Hits.Hits[0].Source == nil {
		return nil, nil
	}
	hit := resp.Hits.Hits[0].Source
	return &trips.CategoryRef{ID: hit.ID, Name: hit.Name}, nil
}

func searchBody(description string) map[string]any {
	return map[string]any{
		"size": 1,
		"query": map[string]any{
			"bool": map[string]any{
				"should": []any{
					map[string]any{"match": map[string]any{"keywords": map[string]any{"query": description, "boost": 10}}},
					map[string]any{"match": map[string]any{"name": map[string]any{"query": description, "boost": 1}}},
					map[string]any{"fuzzy": map[string]any{"keywords": map[string]any{"value": description, "fuzziness": "AUTO", "boost": 5}}},
				},
				"minimum_should_match": 1,
			},
		},
	}
}

func (i *Index) Ensure(ctx context.Context) error {
	res, err := esapi.IndicesExistsRequest{Index: []string{i.index}}.Do(ctx, i.es)
	if err != nil {
		return fmt.Errorf("category: index exists: %w", err)
	}
	drain(res)
	switch res.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusNotFound:
	default:
		return fmt.Errorf("category: index exists: %s", res.Status())
	}

	err = i.do(ctx, esapi.IndicesCreateRequest{Index: i.index, Body: strings.NewReader(createBody)}, nil)
	var esErr *esError
	if errors.As(err, &esErr) && esErr.Type == "resource_already_exists_exception" {
		return nil
	}
	return err
}

func (i *Index) SyncAll(ctx context.Context, categories []trips.Category) error {
	if len(categories) == 0 {
		return nil
	}

	buf := &bytes.Buffer{}
	encoder := json.NewEncoder(buf)
	for _, c := range categories {
		meta := map[string]any{"index": map[string]any{"_index": i.index, "_id": strconv.FormatInt(c.ID, 10)}}
		if err := encoder.Encode(meta); err != nil {
			return fmt.Errorf("category: bulk header: %w", err)
		}
		if err := encoder.Encode(toDocument(c)); err != nil {
			return fmt.Errorf("category: bulk document: %w", err)
		}
	}

	var resp bulkResponse

	if err := i.do(ctx, esapi.BulkRequest{Index: i.index, Body: buf}, &resp); err != nil {
		return err
	}
	return resp.err()
}

type bulkResponse struct {
	Errors bool `json:"errors"`
	Items  []map[string]struct {
		ID     string `json:"_id"`
		Status int    `json:"status"`
		Error  struct {
			Type   string `json:"type"`
			Reason string `json:"reason"`
		} `json:"error"`
	} `json:"items"`
}

func (r bulkResponse) err() error {
	var failed []string
	for _, item := range r.Items {
		for _, result := range item {
			if result.Status >= 300 {
				failed = append(failed, fmt.Sprintf("id %s: %s: %s", result.ID, result.Error.Type, result.Error.Reason))
			}
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("category: bulk rejected %d of %d documents: %s",
			len(failed), len(r.Items), strings.Join(failed, "; "))
	}
	if r.Errors {
		return errors.New("category: bulk reported errors without a failed item")
	}
	return nil
}

func (i *Index) Put(ctx context.Context, category trips.Category) error {
	body, err := json.Marshal(toDocument(category))
	if err != nil {
		return fmt.Errorf("category: document: %w", err)
	}
	req := esapi.IndexRequest{
		Index:      i.index,
		DocumentID: strconv.FormatInt(category.ID, 10),
		Body:       bytes.NewReader(body),
		OpType:     "index",
	}
	return i.do(ctx, req, nil)
}

func (i *Index) AddKeywords(ctx context.Context, id int64, keywords []string) error {
	if len(keywords) == 0 {
		return nil
	}
	body, err := json.Marshal(map[string]any{
		"script": map[string]any{
			"source": addKeywordsScript,
			"params": map[string]any{"keywords": keywords},
		},
	})
	if err != nil {
		return fmt.Errorf("category: update body: %w", err)
	}
	req := esapi.UpdateRequest{
		Index:      i.index,
		DocumentID: strconv.FormatInt(id, 10),
		Body:       bytes.NewReader(body),
	}
	return i.do(ctx, req, nil)
}

type esError struct {
	Status int
	Type   string
	Reason string
}

func (e *esError) Error() string {
	if e.Type == "" {
		return fmt.Sprintf("category: Elasticsearch answered %d", e.Status)
	}
	return fmt.Sprintf("category: Elasticsearch answered %d: %s: %s", e.Status, e.Type, e.Reason)
}

func (i *Index) do(ctx context.Context, req esapi.Request, dst any) error {
	res, err := req.Do(ctx, i.es)
	if err != nil {
		return fmt.Errorf("category: request failed: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		var body struct {
			Error json.RawMessage `json:"error"`
		}
		_ = json.NewDecoder(res.Body).Decode(&body)
		esclient.Drain(res.Body)
		e := &esError{Status: res.StatusCode}
		var detail struct {
			Type   string `json:"type"`
			Reason string `json:"reason"`
		}
		if json.Unmarshal(body.Error, &detail) == nil {
			e.Type, e.Reason = detail.Type, detail.Reason
		}
		return e
	}

	if dst == nil {
		_, _ = io.Copy(io.Discard, res.Body)
		return nil
	}
	if err := json.NewDecoder(res.Body).Decode(dst); err != nil {
		return fmt.Errorf("category: decode response: %w", err)
	}
	return nil
}

func drain(res *esapi.Response) {
	if res.Body != nil {
		_, _ = io.Copy(io.Discard, res.Body)
		res.Body.Close()
	}
}

func (i *Index) Name() string { return i.index }

func (i *Index) IndexHealth(ctx context.Context) (map[string]string, error) {
	return esclient.IndexHealth(ctx, i.es)
}
