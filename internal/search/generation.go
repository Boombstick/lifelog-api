package search

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/elastic/go-elasticsearch/v8/esapi"
)

const generationLayout = "20060102t150405"

var generationSuffix = regexp.MustCompile(`^_\d{8}t\d{6}$`)

type Generation struct {
	Films  string
	People string
}

func (g Generation) Indices() []string { return []string{g.Films, g.People} }

type indexSpec struct {
	alias   string
	mapping string
}

func (c *Client) specs() []indexSpec {
	return []indexSpec{
		{alias: c.FilmsIndex(), mapping: filmsMapping},
		{alias: c.PeopleIndex(), mapping: peopleMapping},
	}
}

func (c *Client) stamp() string { return c.now().UTC().Format(generationLayout) }

func (c *Client) EnsureIndices(ctx context.Context) error {
	stamp := c.stamp()
	for _, spec := range c.specs() {
		exists, err := c.indexExists(ctx, spec.alias)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		body, err := withAlias(spec.mapping, spec.alias)
		if err != nil {
			return err
		}
		if err := c.createIndex(ctx, spec.alias+"_"+stamp, body); err != nil {
			return err
		}
	}
	return nil
}

func withAlias(mapping, alias string) (string, error) {
	var body map[string]any
	if err := json.Unmarshal([]byte(mapping), &body); err != nil {
		return "", fmt.Errorf("search: маппинг: %w", err)
	}
	body["aliases"] = map[string]any{alias: map[string]any{}}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("search: маппинг: %w", err)
	}
	return string(raw), nil
}

func (c *Client) CreateGeneration(ctx context.Context) (Generation, error) {
	stamp := c.stamp()
	gen := Generation{Films: c.FilmsIndex() + "_" + stamp, People: c.PeopleIndex() + "_" + stamp}
	if err := c.createIndex(ctx, gen.Films, filmsMapping); err != nil {
		return Generation{}, err
	}
	if err := c.createIndex(ctx, gen.People, peopleMapping); err != nil {
		if cleanupErr := c.DeleteIndices(context.WithoutCancel(ctx), gen.Films); cleanupErr != nil {
			err = errors.Join(err, cleanupErr)
		}
		return Generation{}, err
	}
	return gen, nil
}

func (c *Client) SwitchTo(ctx context.Context, gen Generation) ([]string, error) {
	var (
		actions []map[string]any
		stale   []string
	)
	for _, pair := range []struct{ alias, target string }{
		{c.FilmsIndex(), gen.Films},
		{c.PeopleIndex(), gen.People},
	} {
		holders, err := c.aliasHolders(ctx, pair.alias)
		if err != nil {
			return nil, err
		}
		if len(holders) == 0 {
			legacy, err := c.indexExists(ctx, pair.alias)
			if err != nil {
				return nil, err
			}
			if legacy {
				actions = append(actions, map[string]any{"remove_index": map[string]any{"index": pair.alias}})
			}
		}
		for _, holder := range holders {
			if holder != pair.target {
				actions = append(actions, map[string]any{"remove": map[string]any{"index": holder, "alias": pair.alias}})
			}
		}
		actions = append(actions, map[string]any{"add": map[string]any{"index": pair.target, "alias": pair.alias}})

		generations, err := c.generations(ctx, pair.alias)
		if err != nil {
			return nil, err
		}

		for _, name := range generations {
			if name < pair.target {
				stale = append(stale, name)
			}
		}
	}

	body, err := json.Marshal(map[string]any{"actions": actions})
	if err != nil {
		return nil, fmt.Errorf("search: тело _aliases: %w", err)
	}
	if err := c.do(esapi.IndicesUpdateAliasesRequest{Body: bytes.NewReader(body)}, ctx, nil); err != nil {
		return nil, err
	}
	return stale, nil
}

func (c *Client) aliasHolders(ctx context.Context, alias string) ([]string, error) {
	var resp map[string]json.RawMessage
	err := c.do(esapi.IndicesGetAliasRequest{Name: []string{alias}}, ctx, &resp)
	if errors.Is(err, errNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	holders := make([]string, 0, len(resp))
	for index := range resp {
		holders = append(holders, index)
	}
	return holders, nil
}

func (c *Client) generations(ctx context.Context, alias string) ([]string, error) {
	var rows []struct {
		Index string `json:"index"`
	}
	req := esapi.CatIndicesRequest{Index: []string{alias + "_*"}, Format: "json", H: []string{"index"}}
	if err := c.do(req, ctx, &rows); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		suffix, ok := strings.CutPrefix(row.Index, alias)
		if ok && generationSuffix.MatchString(suffix) {
			names = append(names, row.Index)
		}
	}
	return names, nil
}

func (c *Client) DeleteIndices(ctx context.Context, names ...string) error {
	if len(names) == 0 {
		return nil
	}
	ignoreMissing := true
	req := esapi.IndicesDeleteRequest{Index: names, IgnoreUnavailable: &ignoreMissing}
	return c.do(req, ctx, nil)
}

func (c *Client) createIndex(ctx context.Context, index, mapping string) error {
	req := esapi.IndicesCreateRequest{Index: index, Body: strings.NewReader(mapping)}
	return c.do(req, ctx, nil)
}

func (c *Client) indexExists(ctx context.Context, index string) (bool, error) {
	res, err := c.es.Indices.Exists([]string{index}, c.es.Indices.Exists.WithContext(ctx))
	if err != nil {
		return false, err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if res.IsError() {
		return false, errFromStatus(res.Status())
	}
	return true, nil
}

func (c *Client) Refresh(ctx context.Context) error {
	return c.RefreshIndices(ctx, c.FilmsIndex(), c.PeopleIndex())
}

func (c *Client) RefreshIndices(ctx context.Context, indices ...string) error {
	return c.do(esapi.IndicesRefreshRequest{Index: indices}, ctx, nil)
}
