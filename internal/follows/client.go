package follows

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"lifelog-api/internal/httpclient"
)

var ErrUnavailable = errors.New("follows: auth unavailable")

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

func NewClient(baseURL, token string, timeout time.Duration) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    httpclient.Internal(timeout),
	}
}

func (c *Client) Following(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	var payload struct {
		Following []uuid.UUID `json:"following"`
	}
	if err := c.call(ctx, http.MethodGet, "/internal/follows/"+userID.String(), nil, &payload); err != nil {
		return nil, err
	}
	return payload.Following, nil
}

func (c *Client) FollowersAmong(ctx context.Context, target uuid.UUID, candidates []uuid.UUID) ([]uuid.UUID, error) {
	if len(candidates) == 0 {
		return nil, nil
	}
	body := map[string]any{"target": target, "candidates": candidates}
	var payload struct {
		Followers []uuid.UUID `json:"followers"`
	}
	if err := c.call(ctx, http.MethodPost, "/internal/follows/check", body, &payload); err != nil {
		return nil, err
	}
	return payload.Followers, nil
}

func (c *Client) Blocked(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	var payload struct {
		Blocked []uuid.UUID `json:"blocked"`
	}
	if err := c.call(ctx, http.MethodGet, "/internal/blocks/"+userID.String(), nil, &payload); err != nil {
		return nil, err
	}
	return payload.Blocked, nil
}

func (c *Client) call(ctx context.Context, method, path string, body, dst any) error {
	if err := httpclient.Call(ctx, c.http, method, c.baseURL+path, c.token, body, dst, http.StatusOK); err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return nil
}
