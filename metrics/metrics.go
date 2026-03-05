// Package metrics defines Prometheus metrics for all tolchain subsystems.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const ns = "tolchain"

// Consensus metrics.
var (
	BlocksProduced = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: ns, Name: "blocks_produced_total",
		Help: "Total blocks produced by this node.",
	})
	BlockHeight = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: ns, Name: "block_height",
		Help: "Current block height.",
	})
	BlockProductionDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: ns, Name: "block_production_duration_seconds",
		Help:    "Time to produce a block.",
		Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1},
	})
	BlockTxCount = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: ns, Name: "block_tx_count",
		Help:    "Transactions per produced block.",
		Buckets: []float64{0, 1, 5, 10, 25, 50, 100, 250, 500},
	})
	TxFailed = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: ns, Name: "tx_failed_total",
		Help: "Transactions that failed during block execution.",
	})
)

// Mempool metrics.
var (
	MempoolSize = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: ns, Name: "mempool_size",
		Help: "Current pending transactions in the mempool.",
	})
	MempoolAdded = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: ns, Name: "mempool_added_total",
		Help: "Total transactions added to the mempool.",
	})
	MempoolRemoved = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: ns, Name: "mempool_removed_total",
		Help: "Total transactions removed from the mempool.",
	})
	MempoolRejected = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: ns, Name: "mempool_rejected_total",
		Help: "Transactions rejected by reason.",
	}, []string{"reason"})
)

// Network metrics.
var (
	PeersConnected = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: ns, Name: "peers_connected",
		Help: "Currently connected peers.",
	})
	P2PMessagesReceived = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: ns, Name: "p2p_messages_received_total",
		Help: "P2P messages received by type.",
	}, []string{"type"})
)

// RPC metrics.
var (
	RPCRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: ns, Name: "rpc_requests_total",
		Help: "RPC requests by method.",
	}, []string{"method"})
	RPCDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: ns, Name: "rpc_request_duration_seconds",
		Help:    "RPC request latency by method.",
		Buckets: []float64{0.0005, 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1},
	}, []string{"method"})
	RPCErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: ns, Name: "rpc_errors_total",
		Help: "RPC errors by error code.",
	}, []string{"code"})
	RPCRateLimited = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: ns, Name: "rpc_rate_limited_total",
		Help: "Requests rejected by rate limiter.",
	})
)

func init() {
	prometheus.MustRegister(
		// consensus
		BlocksProduced, BlockHeight, BlockProductionDuration, BlockTxCount, TxFailed,
		// mempool
		MempoolSize, MempoolAdded, MempoolRemoved, MempoolRejected,
		// network
		PeersConnected, P2PMessagesReceived,
		// rpc
		RPCRequests, RPCDuration, RPCErrors, RPCRateLimited,
	)
}

// Handler returns an http.Handler that serves Prometheus metrics.
func Handler() http.Handler {
	return promhttp.Handler()
}
