package tmdb

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strconv"

	"lifelog-api/internal/safedial"
)

type SearchItem struct {
	ID            int
	MediaType     string
	Title         string
	OriginalTitle string
	Date          Date
	PosterPath    string
	Overview      string
	VoteCount     int
	VoteAverage   float64
}

type SearchPage struct {
	Items      []SearchItem
	Page       int
	TotalPages int
}

type searchResult struct {
	ID            int     `json:"id"`
	MediaType     string  `json:"media_type"`
	Adult         bool    `json:"adult"`
	Title         string  `json:"title"`
	OriginalTitle string  `json:"original_title"`
	ReleaseDate   Date    `json:"release_date"`
	Name          string  `json:"name"`
	OriginalName  string  `json:"original_name"`
	FirstAirDate  Date    `json:"first_air_date"`
	PosterPath    string  `json:"poster_path"`
	Overview      string  `json:"overview"`
	VoteCount     int     `json:"vote_count"`
	VoteAverage   float64 `json:"vote_average"`
}

type searchResponse struct {
	Page       int            `json:"page"`
	TotalPages int            `json:"total_pages"`
	Results    []searchResult `json:"results"`
}

func (c *Client) SearchMulti(ctx context.Context, query string, page int) (SearchPage, error) {
	params := url.Values{}
	params.Set("query", query)
	params.Set("language", "ru-RU")
	params.Set("include_adult", "false")
	params.Set("page", strconv.Itoa(page))

	var resp searchResponse
	if err := c.getJSON(ctx, "/search/multi", params, &resp); err != nil {
		return SearchPage{}, err
	}

	out := SearchPage{Page: resp.Page, TotalPages: resp.TotalPages, Items: make([]SearchItem, 0, len(resp.Results))}
	for _, r := range resp.Results {
		if r.Adult || r.ID <= 0 {
			continue
		}
		item := SearchItem{
			ID:          r.ID,
			MediaType:   r.MediaType,
			PosterPath:  r.PosterPath,
			Overview:    r.Overview,
			VoteCount:   r.VoteCount,
			VoteAverage: r.VoteAverage,
		}
		switch r.MediaType {
		case "movie":
			item.Title, item.OriginalTitle, item.Date = r.Title, r.OriginalTitle, r.ReleaseDate
		case "tv":
			item.Title, item.OriginalTitle, item.Date = r.Name, r.OriginalName, r.FirstAirDate
		default:
			continue
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}

func Unavailable(err error) bool {
	if err == nil || errors.Is(err, ErrNotFound) {
		return false
	}
	if _, ok := errors.AsType[*APIError](err); ok {
		return true
	}
	if _, ok := errors.AsType[*url.Error](err); ok {
		return true
	}
	if _, ok := errors.AsType[net.Error](err); ok {
		return true
	}
	return errors.Is(err, safedial.ErrUnresolved) || errors.Is(err, context.DeadlineExceeded)
}

func (c *Client) PosterURLSize(posterPath, size string) string {
	if posterPath == "" || c.posterBaseURL == "" {
		return ""
	}
	return c.posterBaseURL + "/" + size + posterPath
}
