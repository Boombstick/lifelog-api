package notify

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var enqueued = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "notify_enqueued_total",
	Help: "Push messages put into the outbox, one per device",
}, []string{"kind"})

var deliveries = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "notify_deliveries_total",
	Help: "Outbox delivery attempts by outcome",
}, []string{"channel", "outcome"})
