package search

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/elastic/go-elasticsearch/v8/esapi"
)

type LocalRating struct {
	FilmID uint
	Rating float32
	Votes  int
}

type ratingPatch struct {
	Doc struct {
		LocalRating *float32 `json:"local_rating"`
		LocalVotes  int      `json:"local_votes"`
	} `json:"doc"`
}

func newRatingPatch(rating float32, votes int) ratingPatch {
	var patch ratingPatch
	if votes > 0 {
		patch.Doc.LocalRating = &rating
	}
	patch.Doc.LocalVotes = votes
	return patch
}

func (c *Client) UpdateFilmRating(ctx context.Context, filmID uint, rating float32, votes int) error {
	body, err := json.Marshal(newRatingPatch(rating, votes))
	if err != nil {
		return fmt.Errorf("search: тело обновления рейтинга: %w", err)
	}
	req := esapi.UpdateRequest{
		Index:      c.FilmsIndex(),
		DocumentID: strconv.FormatUint(uint64(filmID), 10),
		Body:       bytes.NewReader(body),
	}
	err = c.do(req, ctx, nil)
	if errors.Is(err, errNotFound) {
		return nil
	}
	return err
}

func (c *Client) UpdateFilmRatings(ctx context.Context, ratings []LocalRating) error {
	if len(ratings) == 0 {
		return nil
	}

	buf := &bytes.Buffer{}
	encoder := json.NewEncoder(buf)
	for _, item := range ratings {
		meta := map[string]any{
			"update": map[string]any{
				"_index": c.FilmsIndex(),
				"_id":    strconv.FormatUint(uint64(item.FilmID), 10),
			},
		}
		if err := encoder.Encode(meta); err != nil {
			return fmt.Errorf("search: заголовок bulk: %w", err)
		}
		if err := encoder.Encode(newRatingPatch(item.Rating, item.Votes)); err != nil {
			return fmt.Errorf("search: документ bulk: %w", err)
		}
	}

	var resp bulkResponse
	if err := c.do(esapi.BulkRequest{Body: buf}, ctx, &resp); err != nil {
		return err
	}
	failed := resp.failures(func(status int) bool { return status == http.StatusNotFound })
	if len(failed) > 0 {
		return fmt.Errorf("search: bulk отклонил обновление рейтинга (%d): %s: %s: %s",
			len(failed), failed[0].ID, failed[0].Type, failed[0].Reason)
	}
	return nil
}
