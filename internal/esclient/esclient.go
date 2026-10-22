package esclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esapi"
)

type Options struct {
	Addresses []string
	Username  string
	Password  string

	PoolSize int

	Transport http.RoundTripper
}

var ErrNoAddress = errors.New("no Elasticsearch address")

func New(opts Options) (*elasticsearch.Client, error) {
	addresses := make([]string, 0, len(opts.Addresses))
	for _, a := range opts.Addresses {
		if a = strings.TrimSpace(a); a != "" {
			addresses = append(addresses, a)
		}
	}
	if len(addresses) == 0 {
		return nil, ErrNoAddress
	}

	transport := opts.Transport
	if transport == nil {
		transport = &http.Transport{
			ResponseHeaderTimeout: 10 * time.Second,
			MaxIdleConns:          opts.PoolSize,
			MaxIdleConnsPerHost:   opts.PoolSize,
			IdleConnTimeout:       5 * time.Minute,
		}
	}

	return elasticsearch.NewClient(elasticsearch.Config{
		Addresses: addresses,
		Username:  opts.Username,
		Password:  opts.Password,
		Transport: transport,
	})
}

func Drain(body io.Reader) {
	_, _ = io.Copy(io.Discard, body)
}

func IndexHealth(ctx context.Context, es *elasticsearch.Client) (map[string]string, error) {
	res, err := esapi.CatIndicesRequest{Format: "json", H: []string{"index", "health"}}.Do(ctx, es)
	if err != nil {
		return nil, fmt.Errorf("cat indices: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		Drain(res.Body)
		return nil, fmt.Errorf("cat indices answered %s", res.Status())
	}
	var rows []struct {
		Index  string `json:"index"`
		Health string `json:"health"`
	}
	if err := json.NewDecoder(res.Body).Decode(&rows); err != nil {
		return nil, fmt.Errorf("cat indices: decode: %w", err)
	}
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		out[row.Index] = row.Health
	}

	aliases, err := catAliases(ctx, es)
	if err != nil {
		return nil, err
	}
	for _, row := range aliases {
		colour, ok := out[row.Index]
		if !ok {
			continue
		}
		if current, seen := out[row.Alias]; !seen || colourRank(colour) > colourRank(current) {
			out[row.Alias] = colour
		}
	}
	return out, nil
}

type aliasRow struct {
	Alias string `json:"alias"`
	Index string `json:"index"`
}

func catAliases(ctx context.Context, es *elasticsearch.Client) ([]aliasRow, error) {
	res, err := esapi.CatAliasesRequest{Format: "json", H: []string{"alias", "index"}}.Do(ctx, es)
	if err != nil {
		return nil, fmt.Errorf("cat aliases: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		Drain(res.Body)
		return nil, fmt.Errorf("cat aliases answered %s", res.Status())
	}
	var rows []aliasRow
	if err := json.NewDecoder(res.Body).Decode(&rows); err != nil {
		return nil, fmt.Errorf("cat aliases: decode: %w", err)
	}
	return rows, nil
}

func colourRank(colour string) int {
	switch colour {
	case "green":
		return 0
	case "yellow":
		return 1
	default:
		return 2
	}
}
