package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	// Namespace defines the namespace for the defines metrics.
	Namespace = "opencloud"

	// Subsystem defines the subsystem for the defines metrics.
	Subsystem = "search"
)

// Metrics defines the available metrics of this service.
type Metrics struct {
	// Counter  *prometheus.CounterVec
	BuildInfo             *prometheus.GaugeVec
	EventsOutstandingAcks prometheus.Gauge
	EventsUnprocessed     prometheus.Gauge
	EventsRedelivered     prometheus.Gauge
}

// New initializes the available metrics.
func New() *Metrics {
	m := &Metrics{
		BuildInfo: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: Namespace,
			Subsystem: Subsystem,
			Name:      "build_info",
			Help:      "Build information",
		}, []string{"version"}),
		EventsOutstandingAcks: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: Namespace,
			Subsystem: Subsystem,
			Name:      "events_outstanding_acks",
			Help:      "Number of outstanding acks for events",
		}),
		EventsUnprocessed: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: Namespace,
			Subsystem: Subsystem,
			Name:      "events_unprocessed",
			Help:      "Number of unprocessed events",
		}),
		EventsRedelivered: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: Namespace,
			Subsystem: Subsystem,
			Name:      "events_redelivered",
			Help:      "Number of redelivered events",
		}),
	}

	_ = prometheus.Register(m.BuildInfo)
	_ = prometheus.Register(m.EventsOutstandingAcks)
	_ = prometheus.Register(m.EventsUnprocessed)
	_ = prometheus.Register(m.EventsRedelivered)

	return m
}
