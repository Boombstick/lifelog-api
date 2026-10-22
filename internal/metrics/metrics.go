package metrics

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var requests = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Обработанные HTTP-запросы",
	},
	[]string{"service", "method", "route", "status"},
)

var durations = promauto.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "Длительность обработки HTTP-запроса",
		Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
	},
	[]string{"service", "method", "route"},
)

const unmatched = "unmatched"

func Middleware(service string) gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		c.Next()

		route := c.FullPath()
		if route == "" {
			route = unmatched
		}
		elapsed := time.Since(started).Seconds()

		requests.WithLabelValues(service, c.Request.Method, route, strconv.Itoa(c.Writer.Status())).Inc()
		durations.WithLabelValues(service, c.Request.Method, route).Observe(elapsed)
	}
}

func Handler() gin.HandlerFunc {
	h := promhttp.Handler()
	return func(c *gin.Context) { h.ServeHTTP(c.Writer, c.Request) }
}
