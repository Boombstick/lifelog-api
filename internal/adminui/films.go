package adminui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"lifelog-api/internal/httpclient"
)

type SyncStatus struct {
	State      string         `json:"state"`
	RunID      string         `json:"run_id"`
	YearFrom   int            `json:"year_from"`
	YearTo     int            `json:"year_to"`
	StartedAt  *time.Time     `json:"started_at"`
	FinishedAt *time.Time     `json:"finished_at"`
	IndexStale bool           `json:"index_stale"`
	Error      string         `json:"error"`
	Stats      map[string]int `json:"stats"`
}

func (s SyncStatus) Running() bool { return s.State == "running" }

type catalogError struct {
	code int
	text string
}

func (e catalogError) Error() string { return e.text }

func statusOf(err error) int {
	var catalog catalogError
	if errors.As(err, &catalog) && catalog.code >= 400 && catalog.code < 500 {
		return catalog.code
	}
	return http.StatusBadGateway
}

type filmsClient struct {
	baseURL string
	client  *http.Client
}

func newFilmsClient(baseURL string) *filmsClient {
	return &filmsClient{baseURL: baseURL, client: httpclient.Internal(15 * time.Second)}
}

func (f *filmsClient) status(ctx context.Context, access string) (*SyncStatus, error) {
	return f.call(ctx, http.MethodGet, access, nil)
}

func (f *filmsClient) start(ctx context.Context, access string, from, to int) (*SyncStatus, error) {
	body, err := json.Marshal(map[string]int{"year_from": from, "year_to": to})
	if err != nil {
		return nil, err
	}
	return f.call(ctx, http.MethodPost, access, body)
}

func (f *filmsClient) call(ctx context.Context, method, access string, body []byte) (*SyncStatus, error) {
	if f.baseURL == "" {
		return nil, fmt.Errorf("адрес каталога не задан: заполните AUTH_FILMS_URL")
	}

	var payload *bytes.Reader
	if body == nil {
		payload = bytes.NewReader(nil)
	} else {
		payload = bytes.NewReader(body)
	}

	request, err := http.NewRequestWithContext(ctx, method, f.baseURL+"/admin/films/sync", payload)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+access)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := f.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("каталог недоступен: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusConflict {
		status := SyncStatus{}
		_ = json.NewDecoder(response.Body).Decode(&status)
		return &status, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var problem struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(response.Body).Decode(&problem)
		if problem.Error != "" {
			return nil, catalogError{code: response.StatusCode, text: "каталог: " + problem.Error}
		}
		return nil, catalogError{code: response.StatusCode, text: fmt.Sprintf("каталог ответил %d", response.StatusCode)}
	}

	status := SyncStatus{}
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("разбор ответа каталога: %w", err)
	}
	return &status, nil
}
