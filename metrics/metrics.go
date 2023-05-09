package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var opWaitSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Namespace: "zenith",
	Name:      "op_wait_seconds",
	Help:      "Time spent waiting for blocking calls to e.g. RPCs, caches, etc.",
}, []string{"op"})

func OpWait(op string, took time.Duration) {
	opWaitSeconds.WithLabelValues(op).Observe(took.Seconds())
}
