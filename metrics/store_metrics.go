package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var ChainInfo = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Namespace: "zenith",
	Name:      "chain_info",
	Help:      "Metadata for all known chains.",
}, []string{"chain_id", "network", "payment_denom", "mekatek_addr"})

var ValidatorInfo = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Namespace: "zenith",
	Name:      "validator_info",
	Help:      "Metadata for all known validators.",
}, []string{"chain_id", "validator_addr", "moniker", "payment_addr"})

var ValidatorCreatedAt = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Namespace: "zenith",
	Name:      "validator_created_at",
	Help:      "UNIX timestamp reflecting created_at for all known validators.",
}, []string{"chain_id", "validator_addr"})

var ValidatorUpdatedAt = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Namespace: "zenith",
	Name:      "validator_updated_at",
	Help:      "UNIX timestamp reflecting updated_at for all known validators.",
}, []string{"chain_id", "validator_addr"})
