package rates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"lifelog-api/internal/trips"
)

const maxResponseBytes = 1 << 20

var errNotConfigured = errors.New("exchange rates: TRIPS_RATES_API_KEY is not configured")

type Client struct {
	baseURL    string
	apiVersion string
	apiKey     string
	http       *http.Client
}

func NewClient(baseURL, apiVersion, apiKey string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{baseURL: baseURL, apiVersion: apiVersion, apiKey: apiKey, http: httpClient}
}

type apiResponse struct {
	Result          string                   `json:"result"`
	ErrorType       string                   `json:"error-type"`
	ConversionRates map[string]trips.Decimal `json:"conversion_rates"`
}

func (c *Client) Latest(ctx context.Context) (map[string]trips.Decimal, error) {
	if c == nil || c.apiKey == "" {
		return nil, errNotConfigured
	}

	endpoint := strings.TrimRight(c.baseURL, "/") + "/v" + c.apiVersion + "/" + url.PathEscape(c.apiKey) + "/latest/USD"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, c.redact(fmt.Errorf("exchange rates: build request: %w", err))
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, c.redact(fmt.Errorf("exchange rates: request: %w", err))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, c.redact(fmt.Errorf("exchange rates: read response: %w", err))
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("exchange rates: provider answered %d%s", resp.StatusCode, errorTypeSuffix(body))
	}

	var parsed apiResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, c.redact(fmt.Errorf("exchange rates: invalid API response: %w", err))
	}

	if len(parsed.ConversionRates) == 0 {
		return nil, fmt.Errorf("exchange rates: invalid API response: no conversion_rates%s", errorTypeSuffix(body))
	}
	return parsed.ConversionRates, nil
}

func errorTypeSuffix(body []byte) string {
	var parsed struct {
		ErrorType string `json:"error-type"`
	}
	if json.Unmarshal(body, &parsed) != nil || parsed.ErrorType == "" {
		return ""
	}
	return " (error-type " + parsed.ErrorType + ")"
}

func (c *Client) redact(err error) error {
	if err == nil || c.apiKey == "" {
		return err
	}
	text := strings.ReplaceAll(err.Error(), c.apiKey, "***")
	text = strings.ReplaceAll(text, url.PathEscape(c.apiKey), "***")
	return &redactedError{text: text, cause: unwrapURLError(err)}
}

type redactedError struct {
	text  string
	cause error
}

func (e *redactedError) Error() string { return e.text }
func (e *redactedError) Unwrap() error { return e.cause }

func unwrapURLError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return nil
}
