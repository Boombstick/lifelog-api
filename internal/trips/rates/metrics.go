package rates

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var snapshotTimestamp = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "trips_exchange_rates_snapshot_timestamp_seconds",
	Help: "Unix time of the exchange rate snapshot in use",
})

var refreshFailures = promauto.NewCounter(prometheus.CounterOpts{
	Name: "trips_exchange_rates_refresh_failures_total",
	Help: "Failed attempts to fetch and store exchange rates, retries included",
})
