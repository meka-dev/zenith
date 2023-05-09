package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var RegisterRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: "zenith",
	Name:      "register_requests_total",
	Help:      "Total number of registration requests seen by the service.",
}, []string{"chain_id", "phase", "result"})

var AuctionRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: "zenith",
	Name:      "auction_requests_total",
	Help:      "Total number of auction requests seen by the service.",
}, []string{"chain_id", "result"})

var BidsSubmittedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: "zenith",
	Name:      "bids_submitted_total",
	Help:      "Total number of bids submitted to the service.",
}, []string{"chain_id", "result"})

var BidTxsSubmittedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: "zenith",
	Name:      "bid_txs_submitted_total",
	Help:      "Total number of bid txs submitted to the service.",
}, []string{"chain_id", "result"})

var BidTxsNormalizedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: "zenith",
	Name:      "bid_txs_normalized_total",
	Help:      "Total number of bid txs that changed after decode/encode during build.",
}, []string{"chain_id"})

var BuildRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: "zenith",
	Name:      "build_requests_total",
	Help:      "Total number of build requests seen by the service.",
}, []string{"chain_id", "result"})

var BidsEvaluatedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: "zenith",
	Name:      "bids_evaluated_total",
	Help:      "Total number of bids evaluated during block building.",
}, []string{"chain_id", "result"})

var BlocksTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: "zenith",
	Name:      "blocks_total",
	Help:      "Total number of blocks built and returned.",
}, []string{"chain_id"})

var BlockTxsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: "zenith",
	Name:      "block_txs_total",
	Help:      "Total number of txs in built and returned blocks.",
}, []string{"chain_id"})

var PaymentsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: "zenith",
	Name:      "payments_total",
	Help:      "Total native-denom payments in built blocks.",
}, []string{"chain_id", "denom", "recipient"})

var ValidatorRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: "zenith",
	Name:      "validator_requests_total",
	Help:      "Requests from registered validators.",
}, []string{"chain_id", "validator_addr", "op", "result"})

var ValidatorLastBuildTimestamp = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Namespace: "zenith",
	Name:      "validator_last_build_timestamp",
	Help:      "UNIX timestamp of most recent build request from validator.",
}, []string{"chain_id", "validator_addr", "result"})
