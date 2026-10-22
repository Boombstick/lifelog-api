package tmdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"lifelog-api/internal/safedial"

	"golang.org/x/time/rate"
)

var ErrNotFound = errors.New("tmdb: resource not found")

type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("tmdb: неожиданный статус %d: %s", e.StatusCode, e.Body)
}

const userAgent = "LifeLog/1.0 (+https://github.com/lifelog; personal project)"

const (
	maxAttempts       = 4
	defaultRetryAfter = 2 * time.Second
)

type Options struct {
	BaseURL string
	Token   string

	RPS float64

	Timeout       time.Duration
	PosterBaseURL string

	PosterSize string
}

type Client struct {
	baseURL       string
	token         string
	useBearer     bool
	httpClient    *http.Client
	limiter       *rate.Limiter
	posterBaseURL string
	posterSize    string

	guest guestSession
}

func NewClient(opts Options) (*Client, error) {
	if strings.TrimSpace(opts.Token) == "" {
		return nil, errors.New("tmdb: не задан TMDB_TOKEN")
	}
	if opts.BaseURL == "" {
		opts.BaseURL = "https://api.themoviedb.org/3"
	}
	if opts.RPS <= 0 {
		opts.RPS = 5
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 10 * time.Second
	}
	if opts.PosterSize == "" {
		opts.PosterSize = "w500"
	}

	useBearer := strings.Count(opts.Token, ".") == 2

	return &Client{
		baseURL:   strings.TrimRight(opts.BaseURL, "/"),
		token:     opts.Token,
		useBearer: useBearer,

		httpClient: &http.Client{
			Timeout:   opts.Timeout,
			Transport: safedial.New(nil).Transport(),
		},

		limiter:       rate.NewLimiter(rate.Limit(opts.RPS), 1),
		posterBaseURL: strings.TrimRight(opts.PosterBaseURL, "/"),
		posterSize:    opts.PosterSize,
	}, nil
}

func (c *Client) PosterURL(posterPath string) string {
	if posterPath == "" || c.posterBaseURL == "" {
		return ""
	}
	return c.posterBaseURL + "/" + c.posterSize + posterPath
}

func (c *Client) Movie(ctx context.Context, id int) (*Movie, error) {
	query := url.Values{}
	query.Set("language", "ru-RU")
	query.Set("append_to_response", "credits,translations")

	var movie Movie
	if err := c.getJSON(ctx, "/movie/"+strconv.Itoa(id), query, &movie); err != nil {
		return nil, err
	}
	return &movie, nil
}

func (c *Client) Genres(ctx context.Context, language string) ([]Genre, error) {
	return c.genres(ctx, "/genre/movie/list", language)
}

func (c *Client) TVGenres(ctx context.Context, language string) ([]Genre, error) {
	return c.genres(ctx, "/genre/tv/list", language)
}

func (c *Client) genres(ctx context.Context, path, language string) ([]Genre, error) {
	query := url.Values{}
	query.Set("language", language)

	var resp genreListResponse
	if err := c.getJSON(ctx, path, query, &resp); err != nil {
		return nil, err
	}
	return resp.Genres, nil
}

func (c *Client) TVDetails(ctx context.Context, id int) (*TVSeries, error) {
	query := url.Values{}
	query.Set("language", "ru-RU")
	query.Set("append_to_response", "aggregate_credits,alternative_titles,external_ids,translations")

	var series TVSeries
	if err := c.getJSON(ctx, "/tv/"+strconv.Itoa(id), query, &series); err != nil {
		return nil, err
	}
	return &series, nil
}

type DiscoverQuery struct {
	From     time.Time
	To       time.Time
	MinVotes int
	Page     int

	Types []int
}

func (c *Client) DiscoverIDs(ctx context.Context, q DiscoverQuery) ([]int, int, int, error) {
	return c.discover(ctx, "/discover/movie", "primary_release_date", q, url.Values{})
}

func (c *Client) DiscoverTV(ctx context.Context, q DiscoverQuery) ([]int, int, int, error) {
	extra := url.Values{}
	if len(q.Types) > 0 {
		types := make([]string, 0, len(q.Types))
		for _, t := range q.Types {
			types = append(types, strconv.Itoa(t))
		}

		extra.Set("with_type", strings.Join(types, "|"))
	}
	return c.discover(ctx, "/discover/tv", "first_air_date", q, extra)
}

func (c *Client) discover(ctx context.Context, path, dateField string, q DiscoverQuery, query url.Values) ([]int, int, int, error) {
	page := q.Page
	if page < 1 {
		page = 1
	}

	query.Set("language", "ru-RU")
	query.Set("include_adult", "false")
	query.Set("sort_by", dateField+".asc")
	query.Set("page", strconv.Itoa(page))
	if !q.From.IsZero() {
		query.Set(dateField+".gte", q.From.Format("2006-01-02"))
	}
	if !q.To.IsZero() {
		query.Set(dateField+".lte", q.To.Format("2006-01-02"))
	}
	if q.MinVotes > 0 {
		query.Set("vote_count.gte", strconv.Itoa(q.MinVotes))
	}

	var resp discoverResponse
	if err := c.getJSON(ctx, path, query, &resp); err != nil {
		return nil, 0, 0, err
	}

	ids := make([]int, 0, len(resp.Results))
	for _, item := range resp.Results {
		ids = append(ids, item.ID)
	}
	return ids, resp.TotalPages, resp.TotalResults, nil
}

func (c *Client) getJSON(ctx context.Context, path string, query url.Values, dst any) error {
	body, err := c.do(ctx, path, query)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return fmt.Errorf("tmdb: разбор ответа %s: %w", path, err)
	}
	return nil
}

func (c *Client) do(ctx context.Context, path string, query url.Values) ([]byte, error) {
	if query == nil {
		query = url.Values{}
	}
	if !c.useBearer {
		query.Set("api_key", c.token)
	}

	endpoint := c.baseURL + path
	if encoded := query.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, err
		}

		body, retryAfter, err := c.attempt(ctx, endpoint)
		switch {
		case err == nil:
			return body, nil
		case errors.Is(err, ErrNotFound):
			return nil, err
		case isClientError(err):
			return nil, err

		case ctx.Err() != nil:
			return nil, ctx.Err()
		}

		lastErr = err
		if attempt == maxAttempts {
			break
		}

		wait := retryAfter
		if wait <= 0 {
			wait = defaultRetryAfter << (attempt - 1)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}

	return nil, fmt.Errorf("tmdb: %s не удался за %d попыток: %w", path, maxAttempts, lastErr)
}

func (c *Client) attempt(ctx context.Context, endpoint string) ([]byte, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	if c.useBearer {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(resp.Body)

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, 0, ErrNotFound
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, parseRetryAfter(resp.Header.Get("Retry-After")), &APIError{
			StatusCode: resp.StatusCode,
			Body:       string(body),
		}
	case resp.StatusCode >= 500:
		return nil, 0, &APIError{StatusCode: resp.StatusCode, Body: string(body)}
	case resp.StatusCode >= 400:

		return nil, 0, fmt.Errorf("%w (ретраи бесполезны)", &APIError{
			StatusCode: resp.StatusCode,
			Body:       string(body),
		})
	}

	if readErr != nil {
		return nil, 0, readErr
	}
	return body, 0, nil
}

func isClientError(err error) bool {
	apiErr, ok := errors.AsType[*APIError](err)
	return ok && apiErr.StatusCode >= 400 && apiErr.StatusCode < 500 && apiErr.StatusCode != http.StatusTooManyRequests
}

func parseRetryAfter(header string) time.Duration {
	if header == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(header); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(header); err == nil {
		if wait := time.Until(at); wait > 0 {
			return wait
		}
	}
	return 0
}

func (c *Client) Person(ctx context.Context, id int) (*Person, error) {
	var person Person
	if err := c.getJSON(ctx, "/person/"+strconv.Itoa(id), url.Values{}, &person); err != nil {
		return nil, err
	}
	return &person, nil
}

func (c *Client) AlternativeTitles(ctx context.Context, movieID int) ([]AlternativeTitle, error) {
	query := url.Values{}
	query.Set("country", "RU")

	var resp AlternativeTitles
	if err := c.getJSON(ctx, "/movie/"+strconv.Itoa(movieID)+"/alternative_titles", query, &resp); err != nil {
		return nil, err
	}
	return resp.Titles, nil
}

func (c *Client) TVAlternativeTitles(ctx context.Context, tvID int) ([]AlternativeTitle, error) {
	query := url.Values{}
	query.Set("country", "RU")

	var resp TVAlternativeTitles
	if err := c.getJSON(ctx, "/tv/"+strconv.Itoa(tvID)+"/alternative_titles", query, &resp); err != nil {
		return nil, err
	}
	return resp.Titles, nil
}

func (c *Client) Recommendations(ctx context.Context, movieID int) ([]int, error) {
	return c.recommendations(ctx, "/movie/"+strconv.Itoa(movieID)+"/recommendations")
}

func (c *Client) TVRecommendations(ctx context.Context, tvID int) ([]int, error) {
	return c.recommendations(ctx, "/tv/"+strconv.Itoa(tvID)+"/recommendations")
}

func (c *Client) recommendations(ctx context.Context, path string) ([]int, error) {
	query := url.Values{}
	query.Set("language", "ru-RU")

	var resp struct {
		Results []struct {
			ID int `json:"id"`
		} `json:"results"`
	}
	if err := c.getJSON(ctx, path, query, &resp); err != nil {
		return nil, err
	}

	ids := make([]int, 0, len(resp.Results))
	for _, item := range resp.Results {
		ids = append(ids, item.ID)
	}
	return ids, nil
}

func (c *Client) Ping(ctx context.Context) error {
	if err := c.limiter.Wait(ctx); err != nil {
		return err
	}
	endpoint := c.baseURL + "/configuration"
	if !c.useBearer {
		endpoint += "?" + url.Values{"api_key": {c.token}}.Encode()
	}
	_, _, err := c.attempt(ctx, endpoint)
	return err
}
