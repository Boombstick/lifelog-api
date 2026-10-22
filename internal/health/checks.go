package health

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

const ThirdPartyTTL = 30 * time.Second

func ErrorText(err error, secrets ...string) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		text = strings.ReplaceAll(text, urlErr.URL, redactURL(urlErr.URL))
	}
	for _, secret := range secrets {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, "***")
		}
	}
	const limit = 300
	if len(text) > limit {
		text = text[:limit] + "..."
	}
	return text
}

func redactURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return "<url>"
	}
	return parsed.Scheme + "://" + parsed.Host
}

type pinger interface {
	PingContext(ctx context.Context) error
}

func Postgres(db pinger) Check {
	return Check{Name: "postgres", Critical: true, Run: func(ctx context.Context) Result {
		if err := db.PingContext(ctx); err != nil {
			return Fail("PostgreSQL is unavailable: " + ErrorText(err))
		}
		return Ok("PostgreSQL is available")
	}}
}

func JWKS(jwksURL string, client *http.Client) Check {
	return Check{Name: "auth-jwks", Critical: true, Run: func(ctx context.Context) Result {
		if jwksURL == "" {
			return Fail("AUTH_JWKS_URL is not configured, bearer tokens are rejected")
		}
		body, err := get(ctx, client, jwksURL)
		if err != nil {
			return Fail("JWKS unavailable: " + ErrorText(err))
		}
		var set struct {
			Keys []struct {
				Kty string `json:"kty"`
			} `json:"keys"`
		}
		if err := json.Unmarshal(body, &set); err != nil {
			return Fail("JWKS is not valid JSON: " + ErrorText(err))
		}
		rsaKeys := 0
		for _, key := range set.Keys {
			if key.Kty == "RSA" {
				rsaKeys++
			}
		}
		if rsaKeys == 0 {
			return Fail("JWKS has no RSA keys")
		}
		return Ok(fmt.Sprintf("JWKS available, %d key(s)", rsaKeys))
	}}
}

func WritableDir(name, dir string) Check {
	return Check{Name: name, Critical: true, Run: func(ctx context.Context) Result {
		info, err := os.Stat(dir)
		if err != nil {
			return Fail("directory is unavailable: " + ErrorText(err))
		}
		if !info.IsDir() {
			return Fail("path is not a directory")
		}
		file, err := os.CreateTemp(dir, ".health-*")
		if err != nil {
			return Fail("directory is not writable: " + ErrorText(err))
		}
		_, writeErr := file.WriteString("ok")
		closeErr := file.Close()
		removeErr := os.Remove(file.Name())
		if err := errors.Join(writeErr, closeErr, removeErr); err != nil {
			return Fail("directory is not writable: " + ErrorText(err))
		}
		return Ok("directory is writable")
	}}
}

type IndexHealth func(ctx context.Context) (map[string]string, error)

func Elasticsearch(critical bool, probe IndexHealth, indices ...string) Check {
	return Check{Name: "elasticsearch", Critical: critical, Run: func(ctx context.Context) Result {
		colours, err := probe(ctx)
		if err != nil {
			return Fail("Elasticsearch is unavailable: " + ErrorText(err))
		}
		return judgeIndices(colours, indices)
	}}
}

func judgeIndices(colours map[string]string, indices []string) Result {
	worst := Healthy
	parts := make([]string, 0, len(indices))
	for _, index := range indices {
		colour, ok := colours[index]
		switch {
		case !ok:
			worst = max(worst, Unhealthy)
			parts = append(parts, index+" missing")
			continue
		case colour == "green":
		case colour == "yellow":
			worst = max(worst, Degraded)
		default:
			worst = max(worst, Unhealthy)
		}
		parts = append(parts, index+" "+colour)
	}
	sort.Strings(parts)
	return Result{Status: worst, Description: "indices: " + strings.Join(parts, ", ")}
}

func Reachable(ctx context.Context, client *http.Client, target string) error {
	_, err := get(ctx, client, target)
	return err
}

func get(ctx context.Context, client *http.Client, target string) ([]byte, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("%s answered %d", redactURL(target), resp.StatusCode)
	}
	return body, nil
}

func Service(db pinger, jwksURL string, checks ...Check) *Registry {
	base := []Check{Postgres(db), JWKS(jwksURL, &http.Client{Timeout: 5 * time.Second})}
	return New(append(base, checks...)...)
}
