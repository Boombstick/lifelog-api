package tmdb

import (
	"net/http"
	"testing"
	"time"
)

func TestParseRetryAfter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		header string
		want   time.Duration
	}{
		{name: "заголовка нет", header: "", want: 0},
		{name: "секунды", header: "5", want: 5 * time.Second},
		{name: "ноль секунд", header: "0", want: 0},
		{name: "большая пауза", header: "120", want: 120 * time.Second},

		{name: "отрицательные секунды", header: "-3", want: 0},
		{name: "не число и не дата", header: "скоро", want: 0},
		{name: "дробные секунды не по спецификации", header: "2.5", want: 0},
		{name: "дата в прошлом", header: "Mon, 02 Jan 2006 15:04:05 GMT", want: 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := parseRetryAfter(tc.header); got != tc.want {
				t.Errorf("parseRetryAfter(%q) = %v, ожидалось %v", tc.header, got, tc.want)
			}
		})
	}
}

func TestParseRetryAfterHTTPDate(t *testing.T) {
	t.Parallel()

	header := time.Now().Add(30 * time.Second).UTC().Format(http.TimeFormat)

	got := parseRetryAfter(header)
	if got < 25*time.Second || got > 31*time.Second {
		t.Errorf("parseRetryAfter(%q) = %v, ожидалось около 30s", header, got)
	}
}

func TestNewClientTokenKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		token         string
		wantBearer    bool
		wantErrorOnly bool
	}{
		{
			name:       "jwt с двумя точками — Bearer",
			token:      "eyJhbGciOiJIUzI1NiJ9.eyJhdWQiOiJ4In0.signature",
			wantBearer: true,
		},
		{
			name:       "ключ v3 без точек — api_key",
			token:      "0123456789abcdef0123456789abcdef",
			wantBearer: false,
		},
		{name: "одна точка — не JWT", token: "abc.def", wantBearer: false},
		{name: "три точки — не JWT", token: "a.b.c.d", wantBearer: false},
		{name: "пустой токен — ошибка", token: "", wantErrorOnly: true},
		{name: "только пробелы — ошибка", token: "   ", wantErrorOnly: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client, err := NewClient(Options{Token: tc.token})
			if tc.wantErrorOnly {
				if err == nil {
					t.Fatal("ожидалась ошибка о незаданном токене, получен клиент")
				}
				if client != nil {
					t.Errorf("при ошибке клиент должен быть nil, получен %+v", client)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			if client.useBearer != tc.wantBearer {
				t.Errorf("useBearer = %v, ожидалось %v", client.useBearer, tc.wantBearer)
			}
		})
	}
}

func TestNewClientDefaults(t *testing.T) {
	t.Parallel()

	client, err := NewClient(Options{Token: "0123456789abcdef0123456789abcdef"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	if client.baseURL != "https://api.themoviedb.org/3" {
		t.Errorf("baseURL = %q, ожидался адрес TMDB по умолчанию", client.baseURL)
	}
	if client.posterSize != "w500" {
		t.Errorf("posterSize = %q, ожидался w500", client.posterSize)
	}
	if client.httpClient.Timeout != 10*time.Second {
		t.Errorf("Timeout = %v, ожидалось 10s", client.httpClient.Timeout)
	}

	if client.limiter.Burst() != 1 {
		t.Errorf("burst = %d, ожидался 1", client.limiter.Burst())
	}
}

func TestNewClientTrimsTrailingSlash(t *testing.T) {
	t.Parallel()

	client, err := NewClient(Options{
		Token:         "0123456789abcdef0123456789abcdef",
		BaseURL:       "https://api.example.com/3/",
		PosterBaseURL: "https://image.example.com/t/p/",
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	if client.baseURL != "https://api.example.com/3" {
		t.Errorf("baseURL = %q", client.baseURL)
	}
	if got := client.PosterURL("/abc.jpg"); got != "https://image.example.com/t/p/w500/abc.jpg" {
		t.Errorf("PosterURL = %q", got)
	}
}

func TestPosterURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		posterBase string
		size       string
		path       string
		want       string
	}{
		{
			name:       "обычный путь",
			posterBase: "https://image.tmdb.org/t/p",
			size:       "w500",
			path:       "/abc123.jpg",
			want:       "https://image.tmdb.org/t/p/w500/abc123.jpg",
		},
		{
			name:       "другой размер",
			posterBase: "https://image.tmdb.org/t/p",
			size:       "original",
			path:       "/abc123.jpg",
			want:       "https://image.tmdb.org/t/p/original/abc123.jpg",
		},
		{
			name:       "пустой путь остаётся пустым",
			posterBase: "https://image.tmdb.org/t/p",
			size:       "w500",
			path:       "",
			want:       "",
		},
		{
			name: "без базового адреса ссылка не собирается",
			size: "w500",
			path: "/abc123.jpg",
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client, err := NewClient(Options{
				Token:         "0123456789abcdef0123456789abcdef",
				PosterBaseURL: tc.posterBase,
				PosterSize:    tc.size,
			})
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			if got := client.PosterURL(tc.path); got != tc.want {
				t.Errorf("PosterURL(%q) = %q, ожидалось %q", tc.path, got, tc.want)
			}
		})
	}
}
