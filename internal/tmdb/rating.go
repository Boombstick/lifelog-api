package tmdb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

type guestSession struct {
	mu        sync.Mutex
	id        string
	expiresAt time.Time
}

type guestSessionResponse struct {
	Success        bool   `json:"success"`
	GuestSessionID string `json:"guest_session_id"`
	ExpiresAt      string `json:"expires_at"`
}

func (c *Client) guestSessionID(ctx context.Context) (string, error) {
	if id, ok := c.cachedGuestSession(); ok {
		return id, nil
	}

	var resp guestSessionResponse
	if err := c.getJSON(ctx, "/authentication/guest_session/new", url.Values{}, &resp); err != nil {
		return "", fmt.Errorf("гостевая сессия TMDB: %w", err)
	}
	if !resp.Success || resp.GuestSessionID == "" {
		return "", fmt.Errorf("гостевая сессия TMDB: пустой ответ")
	}

	expires, err := time.Parse("2006-01-02 15:04:05 MST", resp.ExpiresAt)
	if err != nil {
		expires = time.Now().Add(30 * time.Minute)
	}

	c.guest.mu.Lock()
	defer c.guest.mu.Unlock()

	if c.guest.id != "" && time.Now().Add(time.Minute).Before(c.guest.expiresAt) {
		return c.guest.id, nil
	}
	c.guest.id = resp.GuestSessionID
	c.guest.expiresAt = expires
	return c.guest.id, nil
}

func (c *Client) cachedGuestSession() (string, bool) {
	c.guest.mu.Lock()
	defer c.guest.mu.Unlock()
	if c.guest.id != "" && time.Now().Add(time.Minute).Before(c.guest.expiresAt) {
		return c.guest.id, true
	}
	return "", false
}

func (c *Client) RateMovie(ctx context.Context, movieID int, value float64) error {
	return c.rate(ctx, "/movie/"+strconv.Itoa(movieID)+"/rating", value)
}

func (c *Client) RateTV(ctx context.Context, tvID int, value float64) error {
	return c.rate(ctx, "/tv/"+strconv.Itoa(tvID)+"/rating", value)
}

func (c *Client) rate(ctx context.Context, path string, value float64) error {
	if value < 0.5 || value > 10 {
		return fmt.Errorf("оценка %v вне диапазона TMDB 0.5..10", value)
	}

	session, err := c.guestSessionID(ctx)
	if err != nil {
		return err
	}

	query := url.Values{}
	query.Set("guest_session_id", session)
	if !c.useBearer {
		query.Set("api_key", c.token)
	}

	payload, err := json.Marshal(map[string]float64{"value": value})
	if err != nil {
		return err
	}

	endpoint := c.baseURL + path
	if encoded := query.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json;charset=utf-8")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	if c.useBearer {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	if err := c.limiter.Wait(ctx); err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("отправка оценки в TMDB: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return &APIError{StatusCode: resp.StatusCode, Body: "оценка не принята"}
	}
	return nil
}
