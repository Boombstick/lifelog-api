package calendar

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

const GoogleEventsScope = "https://www.googleapis.com/auth/calendar.events.readonly"

type GoogleConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string

	AuthURL  string
	TokenURL string
	APIURL   string
}

type GoogleGrant struct {
	RefreshToken string
	Email        string
	Scopes       []string
}

type GoogleClient struct {
	oauth  *oauth2.Config
	apiURL string
	http   *http.Client
}

func NewGoogleClient(cfg GoogleConfig) *GoogleClient {
	endpoint := oauth2.Endpoint{
		AuthURL:   "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:  "https://oauth2.googleapis.com/token",
		AuthStyle: oauth2.AuthStyleInParams,
	}
	if cfg.AuthURL != "" {
		endpoint.AuthURL = cfg.AuthURL
	}
	if cfg.TokenURL != "" {
		endpoint.TokenURL = cfg.TokenURL
	}
	apiURL := "https://www.googleapis.com/calendar/v3"
	if cfg.APIURL != "" {
		apiURL = strings.TrimRight(cfg.APIURL, "/")
	}
	return &GoogleClient{
		oauth: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Endpoint:     endpoint,

			Scopes: []string{"openid", "email", GoogleEventsScope},
		},
		apiURL: apiURL,
		http:   &http.Client{Timeout: 20 * time.Second},
	}
}

func (g *GoogleClient) AuthURL(state string) string {
	return g.oauth.AuthCodeURL(state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("prompt", "consent"),
		oauth2.SetAuthURLParam("include_granted_scopes", "true"),
	)
}

func (g *GoogleClient) Exchange(ctx context.Context, code string) (*GoogleGrant, error) {
	token, err := g.oauth.Exchange(g.context(ctx), code)
	if err != nil {
		return nil, fmt.Errorf("google: code exchange: %w", err)
	}
	if token.RefreshToken == "" {
		return nil, errors.New("google: consent returned no refresh token")
	}

	grant := &GoogleGrant{RefreshToken: token.RefreshToken}
	if raw, ok := token.Extra("scope").(string); ok {
		grant.Scopes = strings.Fields(raw)
	}
	if raw, ok := token.Extra("id_token").(string); ok {
		grant.Email = emailFromIDToken(raw)
	}
	return grant, nil
}

func emailFromIDToken(raw string) string {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Email string `json:"email"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return ""
	}
	return claims.Email
}

func (g *GoogleClient) Events(ctx context.Context, refreshToken string, from, to time.Time) ([]ImportedEvent, error) {
	source := g.oauth.TokenSource(g.context(ctx), &oauth2.Token{RefreshToken: refreshToken})
	client := oauth2.NewClient(g.context(ctx), source)

	var out []ImportedEvent
	pageToken := ""

	for page := 0; page < 20; page++ {
		query := url.Values{}
		query.Set("singleEvents", "true")
		query.Set("orderBy", "startTime")
		query.Set("timeMin", from.UTC().Format(time.RFC3339))
		query.Set("timeMax", to.UTC().Format(time.RFC3339))
		query.Set("maxResults", "2500")

		query.Add("eventTypes", "default")
		query.Add("eventTypes", "fromGmail")
		query.Add("eventTypes", "birthday")
		if pageToken != "" {
			query.Set("pageToken", pageToken)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet,
			g.apiURL+"/calendars/primary/events?"+query.Encode(), nil)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			if isRevoked(err) {
				return nil, ErrGoogleRevoked
			}
			return nil, fmt.Errorf("google: list events: %w", err)
		}

		var payload googleEventsPage
		err = decodeGoogle(resp, &payload)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}

		events, err := payload.imported()
		if err != nil {
			return nil, err
		}
		out = append(out, events...)
		if payload.NextPageToken == "" {
			return out, nil
		}
		pageToken = payload.NextPageToken
	}
	return out, nil
}

func (g *GoogleClient) Revoke(ctx context.Context, refreshToken string) error {
	form := url.Values{"token": {refreshToken}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://oauth2.googleapis.com/revoke", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := g.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusBadRequest {
		return fmt.Errorf("google: revoke answered %d", resp.StatusCode)
	}
	return nil
}

func (g *GoogleClient) context(ctx context.Context) context.Context {
	return context.WithValue(ctx, oauth2.HTTPClient, g.http)
}

func isRevoked(err error) bool {
	var retrieve *oauth2.RetrieveError
	return errors.As(err, &retrieve) && retrieve.ErrorCode == "invalid_grant"
}

func decodeGoogle(resp *http.Response, dst any) error {
	switch resp.StatusCode {
	case http.StatusOK:
		return json.NewDecoder(resp.Body).Decode(dst)
	case http.StatusUnauthorized:
		return ErrGoogleRevoked
	case http.StatusForbidden:

		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		if strings.Contains(string(body), "insufficientPermissions") ||
			strings.Contains(string(body), "ACCESS_TOKEN_SCOPE_INSUFFICIENT") {
			return ErrGoogleScopeMissing
		}
		return fmt.Errorf("google: answered 403: %s", strings.TrimSpace(string(body)))
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("google: answered %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
}

type googleEventsPage struct {
	TimeZone      string        `json:"timeZone"`
	NextPageToken string        `json:"nextPageToken"`
	Items         []googleEvent `json:"items"`
}

type googleEvent struct {
	ID          string       `json:"id"`
	Status      string       `json:"status"`
	Summary     string       `json:"summary"`
	Description string       `json:"description"`
	Location    string       `json:"location"`
	HTMLLink    string       `json:"htmlLink"`
	Start       googleMoment `json:"start"`
	End         googleMoment `json:"end"`
	Attendees   []struct {
		Self           bool   `json:"self"`
		ResponseStatus string `json:"responseStatus"`
	} `json:"attendees"`
}

type googleMoment struct {
	Date     string `json:"date"`
	DateTime string `json:"dateTime"`
	TimeZone string `json:"timeZone"`
}

func (p googleEventsPage) imported() ([]ImportedEvent, error) {
	out := make([]ImportedEvent, 0, len(p.Items))
	for _, item := range p.Items {
		event, keep, err := item.imported()
		if err != nil {
			return nil, fmt.Errorf("google: event %s: %w", item.ID, err)
		}
		if keep {
			out = append(out, event)
		}
	}
	return out, nil
}

func (e googleEvent) imported() (ImportedEvent, bool, error) {
	if e.ID == "" || e.Status == "cancelled" {
		return ImportedEvent{}, false, nil
	}

	for _, attendee := range e.Attendees {
		if attendee.Self && attendee.ResponseStatus == "declined" {
			return ImportedEvent{}, false, nil
		}
	}

	event := ImportedEvent{
		GoogleID: e.ID,
		Title:    strings.TrimSpace(e.Summary),
		Details:  e.Description,
		Location: e.Location,
		Link:     e.HTMLLink,
	}

	if event.Title == "" {
		event.Title = "(без названия)"
	}

	if e.Start.Date != "" {
		start, err := time.Parse("2006-01-02", e.Start.Date)
		if err != nil {
			return ImportedEvent{}, false, err
		}
		event.StartsAt, event.AllDay = start, true

		if e.End.Date != "" {
			end, err := time.Parse("2006-01-02", e.End.Date)
			if err != nil {
				return ImportedEvent{}, false, err
			}
			if last := end.AddDate(0, 0, -1); last.After(start) {
				event.EndsAt = &last
			}
		}
		return event, true, nil
	}

	start, err := time.Parse(time.RFC3339, e.Start.DateTime)
	if err != nil {
		return ImportedEvent{}, false, err
	}
	event.StartsAt = start
	if e.End.DateTime != "" {
		end, err := time.Parse(time.RFC3339, e.End.DateTime)
		if err != nil {
			return ImportedEvent{}, false, err
		}
		if end.After(start) {
			event.EndsAt = &end
		}
	}
	return event, true, nil
}
