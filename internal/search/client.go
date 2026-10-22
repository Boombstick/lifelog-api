package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"lifelog-api/internal/esclient"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esapi"
)

const (
	FilmsIndexSuffix  = "films"
	PeopleIndexSuffix = "people"
)

var ErrOverloaded = errors.New("search: cluster overloaded")

var ErrTimeout = errors.New("search: read deadline exceeded")

const DefaultReadTimeout = 5 * time.Second

var errNotFound = errors.New("search: 404 Not Found")

var cacheResults = true

type Client struct {
	es          *elasticsearch.Client
	prefix      string
	readTimeout time.Duration
	now         func() time.Time
}

type Options struct {
	Addresses []string

	Username string
	Password string

	IndexPrefix string

	ReadTimeout time.Duration

	Transport http.RoundTripper
}

func NewClient(opts Options) (*Client, error) {
	es, err := esclient.New(esclient.Options{
		Addresses: opts.Addresses,
		Username:  opts.Username,
		Password:  opts.Password,

		PoolSize:  64,
		Transport: opts.Transport,
	})
	if errors.Is(err, esclient.ErrNoAddress) {
		return nil, fmt.Errorf("search: не задан адрес Elasticsearch")
	}
	if err != nil {
		return nil, fmt.Errorf("search: клиент Elasticsearch: %w", err)
	}

	prefix := opts.IndexPrefix
	if prefix == "" {
		prefix = "lifelog"
	}

	readTimeout := opts.ReadTimeout
	if readTimeout <= 0 {
		readTimeout = DefaultReadTimeout
	}

	return &Client{es: es, prefix: prefix, readTimeout: readTimeout, now: time.Now}, nil
}

func (c *Client) FilmsIndex() string  { return c.prefix + "_" + FilmsIndexSuffix }
func (c *Client) PeopleIndex() string { return c.prefix + "_" + PeopleIndexSuffix }

func (c *Client) Ping(ctx context.Context) error {
	res, err := c.es.Info(c.es.Info.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("search: кластер недоступен: %w", err)
	}
	defer res.Body.Close()
	esclient.Drain(res.Body)

	if res.IsError() {
		return fmt.Errorf("search: кластер ответил %s", res.Status())
	}
	return nil
}

func (c *Client) do(req esapi.Request, ctx context.Context, dst any) error {
	res, err := req.Do(ctx, c.es)
	if err != nil {
		return fmt.Errorf("search: запрос не выполнен: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		var body map[string]any
		_ = json.NewDecoder(res.Body).Decode(&body)
		esclient.Drain(res.Body)
		if res.StatusCode == http.StatusTooManyRequests {
			return fmt.Errorf("%w: %s", ErrOverloaded, describeError(body))
		}
		if res.StatusCode == http.StatusNotFound {
			return fmt.Errorf("%w: %s", errNotFound, describeError(body))
		}
		return fmt.Errorf("search: %s: %s", res.Status(), describeError(body))
	}

	if dst == nil {
		esclient.Drain(res.Body)
		return nil
	}
	if err := json.NewDecoder(res.Body).Decode(dst); err != nil {
		return fmt.Errorf("search: разбор ответа: %w", err)
	}
	esclient.Drain(res.Body)
	return nil
}

func (c *Client) read(req esapi.Request, ctx context.Context, dst any) error {
	readCtx, cancel := context.WithTimeout(ctx, c.readTimeout)
	defer cancel()
	err := c.do(req, readCtx, dst)
	if err != nil && ctx.Err() == nil && errors.Is(readCtx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("%w after %s: %v", ErrTimeout, c.readTimeout, err)
	}
	return err
}

func describeError(body map[string]any) string {
	errValue, ok := body["error"]
	if !ok {
		return "без описания"
	}
	errMap, ok := errValue.(map[string]any)
	if !ok {
		return fmt.Sprint(errValue)
	}

	parts := make([]string, 0, 2)
	if t, ok := errMap["type"].(string); ok {
		parts = append(parts, t)
	}
	if r, ok := errMap["reason"].(string); ok {
		parts = append(parts, r)
	}
	if len(parts) == 0 {
		return "без описания"
	}
	return strings.Join(parts, ": ")
}

func errFromStatus(status string) error {
	return fmt.Errorf("search: кластер ответил %s", status)
}

func (c *Client) IndexHealth(ctx context.Context) (map[string]string, error) {
	return esclient.IndexHealth(ctx, c.es)
}
