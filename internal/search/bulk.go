package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/elastic/go-elasticsearch/v8/esapi"
)

type BulkFailure struct {
	ID     string
	Type   string
	Reason string
}

func (c *Client) IndexFilms(ctx context.Context, docs []FilmDoc) ([]BulkFailure, error) {
	return c.IndexFilmsTo(ctx, c.FilmsIndex(), docs)
}

func (c *Client) IndexFilmsTo(ctx context.Context, index string, docs []FilmDoc) ([]BulkFailure, error) {
	if len(docs) == 0 {
		return nil, nil
	}

	body, err := buildBulk(index, len(docs), func(i int) (uint, any) {
		return docs[i].ID, docs[i]
	})
	if err != nil {
		return nil, err
	}
	return c.bulk(ctx, body)
}

func (c *Client) IndexPeople(ctx context.Context, docs []PersonDoc) ([]BulkFailure, error) {
	return c.IndexPeopleTo(ctx, c.PeopleIndex(), docs)
}

func (c *Client) IndexPeopleTo(ctx context.Context, index string, docs []PersonDoc) ([]BulkFailure, error) {
	if len(docs) == 0 {
		return nil, nil
	}

	body, err := buildBulk(index, len(docs), func(i int) (uint, any) {
		return docs[i].ID, docs[i]
	})
	if err != nil {
		return nil, err
	}
	return c.bulk(ctx, body)
}

func buildBulk(index string, count int, at func(int) (uint, any)) (*bytes.Buffer, error) {
	buf := &bytes.Buffer{}
	encoder := json.NewEncoder(buf)

	for i := range count {
		id, doc := at(i)
		meta := map[string]any{
			"index": map[string]any{
				"_index": index,
				"_id":    strconv.FormatUint(uint64(id), 10),
			},
		}
		if err := encoder.Encode(meta); err != nil {
			return nil, fmt.Errorf("search: заголовок bulk: %w", err)
		}
		if err := encoder.Encode(doc); err != nil {
			return nil, fmt.Errorf("search: документ bulk: %w", err)
		}
	}
	return buf, nil
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

func (r bulkResponse) failures(skip func(status int) bool) []BulkFailure {
	if !r.Errors {
		return nil
	}
	var out []BulkFailure
	for _, item := range r.Items {
		for _, result := range item {
			if result.Status < 300 || (skip != nil && skip(result.Status)) {
				continue
			}
			out = append(out, BulkFailure{ID: result.ID, Type: result.Error.Type, Reason: result.Error.Reason})
		}
	}
	return out
}

func (c *Client) bulk(ctx context.Context, body *bytes.Buffer) ([]BulkFailure, error) {
	var resp bulkResponse
	req := esapi.BulkRequest{Body: body}
	if err := c.do(req, ctx, &resp); err != nil {
		return nil, err
	}
	return resp.failures(nil), nil
}
