package tmdb

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"lifelog-api/internal/film"
)

type changesResponse struct {
	Results []struct {
		ID    int  `json:"id"`
		Adult bool `json:"adult"`
	} `json:"results"`
	Page       int `json:"page"`
	TotalPages int `json:"total_pages"`
}

func (c *Client) ChangedIDs(ctx context.Context, kind film.Kind, start, end time.Time, page int) ([]int, int, error) {
	path := "/movie/changes"
	if kind == film.KindSeries {
		path = "/tv/changes"
	}
	if page < 1 {
		page = 1
	}
	if !start.IsZero() && !end.IsZero() && end.Sub(start) > 14*24*time.Hour {
		return nil, 0, fmt.Errorf("tmdb: окно изменений шире 14 дней (%s — %s)",
			start.Format("2006-01-02"), end.Format("2006-01-02"))
	}

	query := url.Values{}
	query.Set("page", strconv.Itoa(page))
	if !start.IsZero() {
		query.Set("start_date", start.Format("2006-01-02"))
	}
	if !end.IsZero() {
		query.Set("end_date", end.Format("2006-01-02"))
	}

	var resp changesResponse
	if err := c.getJSON(ctx, path, query, &resp); err != nil {
		return nil, 0, err
	}

	ids := make([]int, 0, len(resp.Results))
	for _, item := range resp.Results {
		if item.Adult {
			continue
		}
		ids = append(ids, item.ID)
	}
	return ids, resp.TotalPages, nil
}
