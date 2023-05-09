package pgstore

import (
	"strconv"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type stat interface {
	AcquireCount() int64
	AcquireDuration() time.Duration
	AcquiredConns() int32
	CanceledAcquireCount() int64
	ConstructingConns() int32
	EmptyAcquireCount() int64
	IdleConns() int32
	MaxConns() int32
	TotalConns() int32
}

type statFunc func() stat

type poolCollector struct {
	fn statFunc

	acquireCountDesc         *prometheus.Desc
	acquireDurationDesc      *prometheus.Desc
	acquiredConnsDesc        *prometheus.Desc
	canceledAcquireCountDesc *prometheus.Desc
	constructingConnsDesc    *prometheus.Desc
	emptyAcquireCountDesc    *prometheus.Desc
	idleConnsDesc            *prometheus.Desc
	maxConnsDesc             *prometheus.Desc
	totalConnsDesc           *prometheus.Desc
}

var poolCollectorID uint64

func newPoolCollector(user, host, name string, fn statFunc) *poolCollector {
	var (
		varLabels   = []string{}
		constLabels = prometheus.Labels{
			"db_user":       user,
			"db_host":       host,
			"db_name":       name,
			"db_procpoolid": strconv.FormatUint(atomic.AddUint64(&poolCollectorID, 1), 10),
		}
	)
	return &poolCollector{
		fn: fn,
		acquireCountDesc: prometheus.NewDesc(
			"zenith_pgxpool_acquire_count_total",
			"Cumulative count of successful acquires from the pool.",
			varLabels, constLabels,
		),
		acquireDurationDesc: prometheus.NewDesc(
			"zenith_pgxpool_acquire_count_seconds_total",
			"Total duration of all successful acquires from the pool in nanoseconds.",
			varLabels, constLabels,
		),
		acquiredConnsDesc: prometheus.NewDesc(
			"zenith_pgxpool_acquired_conns",
			"Number of currently acquired connections in the pool.",
			varLabels, constLabels,
		),
		canceledAcquireCountDesc: prometheus.NewDesc(
			"zenith_pgxpool_canceled_acquire_count_total",
			"Cumulative count of acquires from the pool that were canceled by a context.",
			varLabels, constLabels,
		),
		constructingConnsDesc: prometheus.NewDesc(
			"zenith_pgxpool_constructing_conns",
			"Number of conns with construction in progress in the pool.",
			varLabels, constLabels,
		),
		emptyAcquireCountDesc: prometheus.NewDesc(
			"zenith_pgxpool_empty_acquire_count_total",
			"Cumulative count of successful acquires from the pool that waited for a resource to be released or constructed because the pool was empty.",
			varLabels, constLabels,
		),
		idleConnsDesc: prometheus.NewDesc(
			"zenith_pgxpool_idle_conns",
			"Number of currently idle conns in the pool.",
			varLabels, constLabels,
		),
		maxConnsDesc: prometheus.NewDesc(
			"zenith_pgxpool_max_conns",
			"Maximum size of the pool.",
			varLabels, constLabels,
		),
		totalConnsDesc: prometheus.NewDesc(
			"zenith_pgxpool_total_conns",
			"Total number of resources currently in the pool. The value is the sum of ConstructingConns, AcquiredConns, and IdleConns.",
			varLabels, constLabels,
		),
	}
}

func (c *poolCollector) Describe(ch chan<- *prometheus.Desc) {
	prometheus.DescribeByCollect(c, ch)
}

func (c *poolCollector) Collect(metrics chan<- prometheus.Metric) {
	s := c.fn()
	metrics <- prometheus.MustNewConstMetric(
		c.acquireCountDesc,
		prometheus.CounterValue,
		float64(s.AcquireCount()),
	)
	metrics <- prometheus.MustNewConstMetric(
		c.acquireDurationDesc,
		prometheus.CounterValue,
		s.AcquireDuration().Seconds(),
	)
	metrics <- prometheus.MustNewConstMetric(
		c.acquiredConnsDesc,
		prometheus.GaugeValue,
		float64(s.AcquiredConns()),
	)
	metrics <- prometheus.MustNewConstMetric(
		c.canceledAcquireCountDesc,
		prometheus.CounterValue,
		float64(s.CanceledAcquireCount()),
	)
	metrics <- prometheus.MustNewConstMetric(
		c.constructingConnsDesc,
		prometheus.GaugeValue,
		float64(s.ConstructingConns()),
	)
	metrics <- prometheus.MustNewConstMetric(
		c.emptyAcquireCountDesc,
		prometheus.CounterValue,
		float64(s.EmptyAcquireCount()),
	)
	metrics <- prometheus.MustNewConstMetric(
		c.idleConnsDesc,
		prometheus.GaugeValue,
		float64(s.IdleConns()),
	)
	metrics <- prometheus.MustNewConstMetric(
		c.maxConnsDesc,
		prometheus.GaugeValue,
		float64(s.MaxConns()),
	)
	metrics <- prometheus.MustNewConstMetric(
		c.totalConnsDesc,
		prometheus.GaugeValue,
		float64(s.TotalConns()),
	)
}
