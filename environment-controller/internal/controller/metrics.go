package controller

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

// Metrics about what users experience, not what the process is doing.
//
// controller-runtime already gives us reconcile counts, errors and durations
// for free. Those are reconcile-level: a reconcile can fail nine times and the
// environment still ends up fine, so reconcile errors overstate user pain.
// These two are environment-level, which is what an SLO should be built on.
var (
	// The reason label is the same string that goes in the Ready condition, so
	// an alert and `kubectl describe environment` say the same thing.
	environmentFailures = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "environment_reconcile_failures_total",
		Help: "Environment reconcile failures, by reason.",
	}, []string{"reason"})

	// Creation to Ready — the number a developer would recognise as "how long
	// did it take", which is what makes it worth an objective.
	environmentTimeToReady = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "environment_time_to_ready_seconds",
		Help:    "Time from Environment creation to the Ready condition.",
		Buckets: []float64{5, 10, 20, 30, 60, 120, 300, 600},
	})
)

func init() {
	// controller-runtime's registry, so these appear on the manager's existing
	// /metrics endpoint. No second server to run or scrape.
	metrics.Registry.MustRegister(environmentFailures, environmentTimeToReady)
}
