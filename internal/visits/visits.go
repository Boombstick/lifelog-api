package visits

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"lifelog-api/internal/config"
	"lifelog-api/internal/httpclient"
)

type Event struct {
	IP         string    `json:"ip"`
	At         time.Time `json:"at"`
	Service    string    `json:"service"`
	Method     string    `json:"method"`
	Path       string    `json:"path"`
	Status     int       `json:"status"`
	DurationMS float32   `json:"duration_ms"`
	UserAgent  string    `json:"user_agent"`
}

type Sink interface {
	Record(ctx context.Context, events []Event) error
}

const (
	bufferSize    = 5000
	flushEvery    = 10 * time.Second
	userAgentMax  = 200
	pathMax       = 200
	realIPHeader  = "X-Real-IP"
	monitoringUA  = "Blackbox Exporter"
	internalPrefx = "/internal/"
)

type Recorder struct {
	service string
	sink    Sink
	logger  *slog.Logger

	mu      sync.Mutex
	pending []Event
	dropped int
}

func NewRecorder(service string, sink Sink, logger *slog.Logger) *Recorder {
	return &Recorder{service: service, sink: sink, logger: logger}
}

func (r *Recorder) add(e Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.pending) >= bufferSize {
		r.dropped++
		return
	}
	r.pending = append(r.pending, e)
}

func (r *Recorder) take() ([]Event, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	events, dropped := r.pending, r.dropped
	r.pending, r.dropped = nil, 0
	return events, dropped
}

func (r *Recorder) Flush(ctx context.Context) {
	events, dropped := r.take()
	if dropped > 0 {
		r.logger.Warn("visit events dropped, buffer full", "count", dropped)
	}
	if len(events) == 0 {
		return
	}
	if err := r.sink.Record(ctx, events); err != nil {
		r.logger.Warn("visit events not recorded", "count", len(events), "error", err)
	}
}

func (r *Recorder) Run(ctx context.Context) error {
	ticker := time.NewTicker(flushEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			final, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			r.Flush(final)
			cancel()
			return nil
		case <-ticker.C:
			r.Flush(ctx)
		}
	}
}

func Wrap(next http.Handler, r *Recorder) http.Handler {
	if r == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ip := req.Header.Get(realIPHeader)
		if ip == "" || skipped(req) {
			next.ServeHTTP(w, req)
			return
		}
		started := time.Now()
		recorder := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, req)
		r.add(Event{
			IP:         ip,
			At:         started.UTC(),
			Service:    r.service,
			Method:     req.Method,
			Path:       SanitizePath(req.URL.Path),
			Status:     recorder.status,
			DurationMS: float32(time.Since(started).Microseconds()) / 1000,
			UserAgent:  truncate(req.UserAgent(), userAgentMax),
		})
	})
}

func skipped(req *http.Request) bool {
	path := req.URL.Path
	switch path {
	case "/healthz", "/health", "/metrics":
		return true
	}
	return strings.HasPrefix(path, internalPrefx) || strings.Contains(path, internalPrefx) ||
		strings.Contains(req.UserAgent(), monitoringUA)
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

var (
	numeric = regexp.MustCompile(`^\d+$`)
	uuidish = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	tokenly = regexp.MustCompile(`^[A-Za-z0-9_\-.=]{20,}$`)
)

func SanitizePath(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		switch {
		case numeric.MatchString(part), uuidish.MatchString(part):
			parts[i] = ":id"
		case tokenly.MatchString(part):
			parts[i] = ":token"
		}
	}
	return truncate(strings.Join(parts, "/"), pathMax)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8Start(s[cut]) {
		cut--
	}
	return s[:cut]
}

func utf8Start(b byte) bool { return b&0xC0 != 0x80 }

type HTTPSink struct {
	url   string
	token string
	http  *http.Client
}

func NewHTTPSink(authURL, token string) *HTTPSink {
	return &HTTPSink{url: strings.TrimRight(authURL, "/") + "/internal/visits", token: token, http: httpclient.Internal(10 * time.Second)}
}

const sinkBatch = 1000

func (s *HTTPSink) Record(ctx context.Context, events []Event) error {
	for start := 0; start < len(events); start += sinkBatch {
		if err := s.post(ctx, events[start:min(start+sinkBatch, len(events))]); err != nil {
			return err
		}
	}
	return nil
}

func (s *HTTPSink) post(ctx context.Context, events []Event) error {
	body, err := json.Marshal(Batch{Events: events})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(httpclient.InternalTokenHeader, s.token)
	response, err := s.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode/100 != 2 {
		return fmt.Errorf("auth answered %d", response.StatusCode)
	}
	return nil
}

type Batch struct {
	Events []Event `json:"events"`
}

func ForService(c *config.Config, service string, logger *slog.Logger) *Recorder {
	if !c.VisitsEnabled {
		return nil
	}
	return NewRecorder(service, NewHTTPSink(c.VisitsAuthURL, c.AuthInternalToken), logger)
}

func (r *Recorder) Jobs() []func(context.Context) error {
	if r == nil {
		return nil
	}
	return []func(context.Context) error{r.Run}
}
