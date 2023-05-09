package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	httpBuckets = []float64{
		0.001, 0.002, 0.003, 0.004, 0.005, 0.006, 0.007, 0.008, 0.009, // 1ms - 9ms
		0.010, 0.020, 0.030, 0.040, 0.050, 0.060, 0.070, 0.080, 0.090, // 10ms - 90ms
		0.100, 0.125, 0.150, 0.175, 0.200, 0.300, 0.400, 0.500, 0.700, 0.800, 0.900, // 100ms - 900ms
		1.000, 2.000, 4.000, 8.000, // 1s - 8s+
	}

	HTTPRequestDurationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace:   "zenith",
		Name:        "http_request_duration_seconds",
		Help:        "HTTP request duration in seconds.",
		ConstLabels: prometheus.Labels{},
		Buckets:     httpBuckets,
	}, []string{"route", "code"})
)
