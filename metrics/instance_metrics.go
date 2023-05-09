package metrics

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"zenith/build"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var startTimeUnix = time.Now().UTC().Unix()

var _ = promauto.NewGaugeFunc(prometheus.GaugeOpts{
	Namespace: "zenith",
	Name:      "build_info",
	Help:      "Build-time const metadata for this instance.",
	ConstLabels: prometheus.Labels{
		"build_version": build.Version,
		"build_date":    build.Date,
	},
}, func() float64 { return 1.0 })

var _ = promauto.NewGaugeFunc(prometheus.GaugeOpts{
	Namespace: "zenith",
	Name:      "instance_info",
	Help:      "Run-time const metadata for this instance.",
	ConstLabels: prometheus.Labels{
		"network": instanceNetwork,
	},
}, func() float64 { return 1 })

var _ = promauto.NewGaugeFunc(prometheus.GaugeOpts{
	Namespace:   "zenith",
	Name:        "start_timestamp",
	Help:        "UNIX timestamp (UTC) when instance started.",
	ConstLabels: prometheus.Labels{},
}, func() float64 { return float64(startTimeUnix) })

var _ = promauto.NewCounterFunc(prometheus.CounterOpts{
	Namespace:   "zenith",
	Name:        "up_seconds_total",
	Help:        "Total seconds this instance has been up, meant for use with `resets()`.",
	ConstLabels: prometheus.Labels{},
}, func() float64 { return float64(time.Now().UTC().Unix() - startTimeUnix) })

//
//
//

var instanceNetwork = func() string {
	const defaultInstanceNetwork = "unknown-network"

	// Best case: -ldflags '-X build.Network=osmosis'
	if build.Network != "" {
		return build.Network
	}

	// Otherwise: try to extract `xxx` from executable named `zenith-xxx[-<suffix>]`
	exe, err := os.Executable()
	if err != nil {
		return defaultInstanceNetwork
	}

	exe = strings.TrimPrefix(filepath.Base(exe), "zenith-")
	if idx := strings.IndexAny(exe, "-_./"); idx > 0 {
		exe = exe[:idx]
	}
	return exe
}()
