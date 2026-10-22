package health

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type Status int

const (
	Healthy Status = iota
	Degraded
	Unhealthy
)

func (s Status) String() string {
	switch s {
	case Healthy:
		return "Healthy"
	case Degraded:
		return "Degraded"
	default:
		return "Unhealthy"
	}
}

func (s Status) MarshalJSON() ([]byte, error) { return json.Marshal(s.String()) }

type Duration time.Duration

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).Round(100 * time.Microsecond).String())
}

type Result struct {
	Status      Status
	Description string
}

func Ok(description string) Result   { return Result{Healthy, description} }
func Warn(description string) Result { return Result{Degraded, description} }
func Fail(description string) Result { return Result{Unhealthy, description} }

const DefaultTimeout = 2 * time.Second

type Check struct {
	Name string

	Critical bool

	Timeout time.Duration
	Run     func(ctx context.Context) Result
}

type Entry struct {
	Name        string   `json:"name"`
	Status      Status   `json:"status"`
	Description string   `json:"description"`
	Duration    Duration `json:"duration"`
}

type Report struct {
	Status   Status   `json:"status"`
	Duration Duration `json:"duration"`
	Checks   []Entry  `json:"checks"`
}

type Registry struct {
	checks []Check
}

func New(checks ...Check) *Registry { return &Registry{checks: checks} }

func (r *Registry) Run(ctx context.Context) Report {
	started := time.Now()
	entries := make([]Entry, len(r.checks))

	var wg sync.WaitGroup
	for i, check := range r.checks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			entries[i] = runOne(ctx, check)
		}()
	}
	wg.Wait()

	return Report{
		Status:   fold(r.checks, entries),
		Duration: Duration(time.Since(started)),
		Checks:   entries,
	}
}

func fold(checks []Check, entries []Entry) Status {
	overall := Healthy
	for i, entry := range entries {
		status := entry.Status
		if !checks[i].Critical && status > Degraded {
			status = Degraded
		}
		overall = max(overall, status)
	}
	return overall
}

func runOne(parent context.Context, check Check) Entry {
	timeout := check.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	started := time.Now()
	done := make(chan Result, 1)
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				done <- Fail(fmt.Sprintf("check panicked: %v", recovered))
			}
		}()
		done <- check.Run(ctx)
	}()

	var result Result
	select {
	case result = <-done:
	case <-ctx.Done():
		result = Fail(fmt.Sprintf("timed out after %s", timeout))
	}
	return Entry{
		Name:        check.Name,
		Status:      result.Status,
		Description: result.Description,
		Duration:    Duration(time.Since(started)),
	}
}

func (r *Registry) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		report := r.Run(c.Request.Context())
		code := http.StatusOK
		if report.Status == Unhealthy {
			code = http.StatusServiceUnavailable
		}
		c.Header("Cache-Control", "no-store")
		c.JSON(code, report)
	}
}

func Cached(ttl time.Duration, run func(ctx context.Context) Result) func(ctx context.Context) Result {
	c := &cache{ttl: ttl, run: run, now: time.Now, sem: make(chan struct{}, 1)}
	return c.get
}

type cache struct {
	ttl time.Duration
	run func(ctx context.Context) Result
	now func() time.Time
	sem chan struct{}

	result Result
	at     time.Time
	filled bool
}

func (c *cache) get(ctx context.Context) Result {
	select {
	case c.sem <- struct{}{}:
	case <-ctx.Done():
		return Fail("timed out waiting for a check in flight")
	}
	defer func() { <-c.sem }()

	if c.filled && c.now().Sub(c.at) < c.ttl {
		return c.result
	}
	c.result = c.run(ctx)
	c.at = c.now()
	c.filled = true
	return c.result
}
