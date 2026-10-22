package tmdb

import (
	"context"
	"net/url"
)

type FindResult struct {
	MovieIDs []int
	TVIDs    []int
	Other    bool
}

type findResponse struct {
	MovieResults     []struct{ ID int } `json:"movie_results"`
	TVResults        []struct{ ID int } `json:"tv_results"`
	TVEpisodeResults []struct{ ID int } `json:"tv_episode_results"`
	TVSeasonResults  []struct{ ID int } `json:"tv_season_results"`
	PersonResults    []struct{ ID int } `json:"person_results"`
}

func (c *Client) FindByIMDbID(ctx context.Context, imdbID string) (FindResult, error) {
	query := url.Values{}
	query.Set("external_source", "imdb_id")

	var resp findResponse
	if err := c.getJSON(ctx, "/find/"+url.PathEscape(imdbID), query, &resp); err != nil {
		return FindResult{}, err
	}

	result := FindResult{
		Other: len(resp.TVEpisodeResults) > 0 || len(resp.TVSeasonResults) > 0 || len(resp.PersonResults) > 0,
	}
	for _, item := range resp.MovieResults {
		if item.ID > 0 {
			result.MovieIDs = append(result.MovieIDs, item.ID)
		}
	}
	for _, item := range resp.TVResults {
		if item.ID > 0 {
			result.TVIDs = append(result.TVIDs, item.ID)
		}
	}
	return result, nil
}
