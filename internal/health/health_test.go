package health

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func fixed(r Result) func(context.Context) Result {
	return func(context.Context) Result { return r }
}

func TestOverallIsWorstCriticalStatus(t *testing.T) {
	cases := []struct {
		name   string
		checks []Check
		want   Status
	}{
		{"all healthy", []Check{
			{Name: "a", Critical: true, Run: fixed(Ok("ok"))},
			{Name: "b", Critical: true, Run: fixed(Ok("ok"))},
		}, Healthy},
		{"degraded wins over healthy", []Check{
			{Name: "a", Critical: true, Run: fixed(Ok("ok"))},
			{Name: "b", Critical: true, Run: fixed(Warn("yellow"))},
		}, Degraded},
		{"unhealthy critical wins", []Check{
			{Name: "a", Critical: true, Run: fixed(Warn("yellow"))},
			{Name: "b", Critical: true, Run: fixed(Fail("down"))},
		}, Unhealthy},
		{"non-critical failure is capped", []Check{
			{Name: "a", Critical: true, Run: fixed(Ok("ok"))},
			{Name: "b", Critical: false, Run: fixed(Fail("down"))},
		}, Degraded},
		{"non-critical degraded stays degraded", []Check{
			{Name: "a", Critical: true, Run: fixed(Ok("ok"))},
			{Name: "b", Critical: false, Run: fixed(Warn("slow"))},
		}, Degraded},
		{"no checks", nil, Healthy},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			report := New(tc.checks...).Run(context.Background())
			if report.Status != tc.want {
				t.Fatalf("status = %s, want %s", report.Status, tc.want)
			}
		})
	}
}

func TestNonCriticalEntryKeepsItsOwnStatus(t *testing.T) {
	report := New(Check{Name: "tmdb", Run: fixed(Fail("down"))}).Run(context.Background())
	if report.Checks[0].Status != Unhealthy {
		t.Fatalf("entry status = %s, want Unhealthy", report.Checks[0].Status)
	}
	if report.Status != Degraded {
		t.Fatalf("overall = %s, want Degraded", report.Status)
	}
}

func TestChecksKeepRegistrationOrder(t *testing.T) {
	report := New(
		Check{Name: "slow", Run: func(context.Context) Result { time.Sleep(20 * time.Millisecond); return Ok("") }},
		Check{Name: "fast", Run: fixed(Ok(""))},
	).Run(context.Background())
	if report.Checks[0].Name != "slow" || report.Checks[1].Name != "fast" {
		t.Fatalf("order = %s, %s", report.Checks[0].Name, report.Checks[1].Name)
	}
}

func TestChecksRunInParallel(t *testing.T) {
	sleep := func(context.Context) Result { time.Sleep(100 * time.Millisecond); return Ok("") }
	started := time.Now()
	New(
		Check{Name: "a", Run: sleep},
		Check{Name: "b", Run: sleep},
		Check{Name: "c", Run: sleep},
	).Run(context.Background())
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("three 100 ms checks took %s, not parallel", elapsed)
	}
}

func TestTimeoutIsUnhealthy(t *testing.T) {
	block := func(context.Context) Result { time.Sleep(time.Second); return Ok("late") }

	critical := New(Check{Name: "db", Critical: true, Timeout: 20 * time.Millisecond, Run: block}).
		Run(context.Background())
	if critical.Status != Unhealthy || critical.Checks[0].Status != Unhealthy {
		t.Fatalf("critical timeout: overall %s, entry %s", critical.Status, critical.Checks[0].Status)
	}
	if !strings.Contains(critical.Checks[0].Description, "timed out") {
		t.Fatalf("description = %q", critical.Checks[0].Description)
	}
	if time.Duration(critical.Duration) > 500*time.Millisecond {
		t.Fatalf("registry waited %s for a timed-out check", time.Duration(critical.Duration))
	}

	optional := New(Check{Name: "tmdb", Timeout: 20 * time.Millisecond, Run: block}).
		Run(context.Background())
	if optional.Status != Degraded {
		t.Fatalf("non-critical timeout: overall %s, want Degraded", optional.Status)
	}
}

func TestPanicIsUnhealthy(t *testing.T) {
	report := New(Check{Name: "x", Critical: true, Run: func(context.Context) Result { panic("boom") }}).
		Run(context.Background())
	if report.Status != Unhealthy {
		t.Fatalf("status = %s", report.Status)
	}
}

func TestHandlerMapsStatusToCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		result Result
		code   int
	}{
		{Ok("ok"), http.StatusOK},
		{Warn("yellow"), http.StatusOK},
		{Fail("down"), http.StatusServiceUnavailable},
	}
	for _, tc := range cases {
		engine := gin.New()
		engine.GET("/health", New(Check{Name: "postgres", Critical: true, Run: fixed(tc.result)}).Handler())
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
		if rec.Code != tc.code {
			t.Fatalf("%s: code = %d, want %d", tc.result.Status, rec.Code, tc.code)
		}

		var body struct {
			Status   string `json:"status"`
			Duration string `json:"duration"`
			Checks   []struct {
				Name        string `json:"name"`
				Status      string `json:"status"`
				Description string `json:"description"`
				Duration    string `json:"duration"`
			} `json:"checks"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Status != tc.result.Status.String() || len(body.Checks) != 1 ||
			body.Checks[0].Name != "postgres" || body.Checks[0].Description != tc.result.Description ||
			body.Duration == "" || body.Checks[0].Duration == "" {
			t.Fatalf("body = %s", rec.Body.String())
		}
	}
}

func TestDurationFormat(t *testing.T) {
	cases := map[time.Duration]string{
		3940 * time.Microsecond:         `"3.9ms"`,
		204890 * time.Microsecond:       `"204.9ms"`,
		1200 * time.Millisecond:         `"1.2s"`,
		40 * time.Microsecond:           `"0s"`,
		60 * time.Microsecond:           `"100µs"`,
		2*time.Second + time.Nanosecond: `"2s"`,
	}
	for in, want := range cases {
		got, err := json.Marshal(Duration(in))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Errorf("%s: got %s, want %s", in, got, want)
		}
	}
}

func TestCachedReusesResultWithinTTL(t *testing.T) {
	calls := 0
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	c := &cache{
		ttl: 30 * time.Second,
		run: func(context.Context) Result {
			calls++
			return Ok(fmt.Sprint(calls))
		},
		now: func() time.Time { return now },
		sem: make(chan struct{}, 1),
	}

	first := c.get(context.Background())
	now = now.Add(29 * time.Second)
	second := c.get(context.Background())
	if calls != 1 || second != first {
		t.Fatalf("within ttl: calls = %d, second = %+v", calls, second)
	}
	now = now.Add(2 * time.Second)
	third := c.get(context.Background())
	if calls != 2 || third.Description != "2" {
		t.Fatalf("after ttl: calls = %d, third = %+v", calls, third)
	}
}

func TestCachedCachesFailuresToo(t *testing.T) {
	calls := 0
	run := Cached(time.Minute, func(context.Context) Result { calls++; return Fail("down") })
	run(context.Background())
	run(context.Background())
	if calls != 1 {
		t.Fatalf("calls = %d, a failing third party was hit again", calls)
	}
}

func TestErrorTextHidesSecrets(t *testing.T) {
	err := fmt.Errorf("tmdb: %w", &url.Error{
		Op:  "Get",
		URL: "https://api.themoviedb.org/3/configuration?api_key=deadbeef",
		Err: errors.New("dial tcp 127.0.0.1:443: connect: connection refused"),
	})
	text := ErrorText(err)
	if strings.Contains(text, "deadbeef") || strings.Contains(text, "/configuration") {
		t.Fatalf("url leaked: %q", text)
	}
	if !strings.Contains(text, "https://api.themoviedb.org") || !strings.Contains(text, "connection refused") {
		t.Fatalf("lost the useful part: %q", text)
	}

	masked := ErrorText(errors.New("relay said: bad secret s3cr3t for bot123:abc"), "s3cr3t", "123:abc", "")
	if strings.Contains(masked, "s3cr3t") || strings.Contains(masked, "123:abc") {
		t.Fatalf("secret leaked: %q", masked)
	}
}

func TestJudgeIndices(t *testing.T) {
	cases := []struct {
		colours map[string]string
		want    Status
	}{
		{map[string]string{"films": "green", "people": "green", "other": "red"}, Healthy},
		{map[string]string{"films": "green", "people": "yellow"}, Degraded},
		{map[string]string{"films": "red", "people": "green"}, Unhealthy},
		{map[string]string{"films": "green"}, Unhealthy},
	}
	for _, tc := range cases {
		got := judgeIndices(tc.colours, []string{"films", "people"})
		if got.Status != tc.want {
			t.Errorf("%v: %s (%s), want %s", tc.colours, got.Status, got.Description, tc.want)
		}
	}
}
