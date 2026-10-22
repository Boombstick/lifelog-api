package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"
)

const idleConnTimeout = 30 * time.Second

func Internal(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.IdleConnTimeout = idleConnTimeout
	return &http.Client{Timeout: timeout, Transport: transport}
}

const InternalTokenHeader = "X-Internal-Token"

const (
	errorBodyLimit = 512
	drainLimit     = 64 << 10
)

type StatusError struct {
	Method string
	URL    string
	Status int

	Body string
}

func (e *StatusError) Error() string {
	text := fmt.Sprintf("%s %s answered %d", e.Method, e.URL, e.Status)
	if e.Body != "" {
		text += ": " + e.Body
	}
	return text
}

func Call(ctx context.Context, client *http.Client, method, url, token string, body, dst any, ok ...int) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("%s %s: encode body: %w", method, url, err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set(InternalTokenHeader, token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, drainLimit))
		_ = resp.Body.Close()
	}()

	if !accepted(resp.StatusCode, ok) {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, errorBodyLimit))
		return &StatusError{Method: method, URL: url, Status: resp.StatusCode, Body: strings.TrimSpace(string(raw))}
	}
	if dst != nil {
		if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
			return fmt.Errorf("%s %s: decode answer: %w", method, url, err)
		}
	}
	return nil
}

func accepted(status int, ok []int) bool {
	if len(ok) == 0 {
		return status >= 200 && status < 300
	}
	return slices.Contains(ok, status)
}
