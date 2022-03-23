package metrics

import (
	"os"

	"github.com/prometheus/client_golang/prometheus"
)

const metricsEnv = "CATTLE_PROMETHEUS_METRICS"

var prometheusMetrics = false

/*
prometheus

It provides metrics primitives to instrument code for monitoring
It also offers a registry for metrics
*/
var (
	// NewCounterVec creates a new CounterVec based on the provided CounterOpts and
	// partitioned by the given label names.

	/*
			exports metrics Counter(计数器
			计数器是一种累积度量，表示单个单调递增的计数器，其值只能在重新启动时增加或重置为零。
			例如，您可以使用计数器来表示已服务的请求数、已完成的任务数或错误数。
			不要使用计数器暴露可能减少的值

		其它类型metric：
		Gauge：标尺是一种度量，它代表一个可以任意上下变化的数值。仪表通常用于测量温度或当前内存使用情况等值，但也可以“计数”，如并发请求的数量。
		Histogram：直方图对观察结果进行采样(通常是请求持续时间或响应大小)。它还提供了所有观测值的总和。
		Summary：类似于直方图，摘要采样观察结果(通常是请求持续时间和响应大小)。虽然它还提供了观察总数和所有观察值的总和，但它在滑动时间窗口上计算可配置的分位数。
	*/

	// 定义若干个计数器（只增
	TotalAddWS = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: "session_server",
			Name:      "total_add_websocket_session",
			Help:      "Total count of added websocket sessions",
		},
		[]string{"clientkey", "peer"})

	TotalRemoveWS = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: "session_server",
			Name:      "total_remove_websocket_session",
			Help:      "Total count of removed websocket sessions",
		},
		[]string{"clientkey", "peer"})

	TotalAddConnectionsForWS = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: "session_server",
			Name:      "total_add_connections",
			Help:      "Total count of added connections",
		},
		[]string{"clientkey", "proto", "addr"},
	)

	TotalRemoveConnectionsForWS = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: "session_server",
			Name:      "total_remove_connections",
			Help:      "Total count of removed connections",
		},
		[]string{"clientkey", "proto", "addr"},
	)

	TotalTransmitBytesOnWS = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: "session_server",
			Name:      "total_transmit_bytes",
			Help:      "Total bytes transmitted",
		},
		[]string{"clientkey"},
	)

	TotalTransmitErrorBytesOnWS = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: "session_server",
			Name:      "total_transmit_error_bytes",
			Help:      "Total error bytes transmitted",
		},
		[]string{"clientkey"},
	)

	TotalReceiveBytesOnWS = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: "session_server",
			Name:      "total_receive_bytes",
			Help:      "Total bytes received",
		},
		[]string{"clientkey"},
	)

	TotalAddPeerAttempt = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: "session_server",
			Name:      "total_peer_ws_attempt",
			Help:      "Total count of attempts to establish websocket session to other rancher-server",
		},
		[]string{"peer"},
	)
	TotalPeerConnected = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: "session_server",
			Name:      "total_peer_ws_connected",
			Help:      "Total count of connected websocket sessions to other rancher-server",
		},
		[]string{"peer"},
	)
	TotalPeerDisConnected = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: "session_server",
			Name:      "total_peer_ws_disconnected",
			Help:      "Total count of disconnected websocket sessions from other rancher-server",
		},
		[]string{"peer"},
	)
)

// Register registers a series of session
// metrics for Prometheus.
func Register() {
	// Metrics have to be registered to be exposed:

	prometheusMetrics = true

	// Session metrics
	prometheus.MustRegister(TotalAddWS)
	prometheus.MustRegister(TotalRemoveWS)
	prometheus.MustRegister(TotalAddConnectionsForWS)
	prometheus.MustRegister(TotalRemoveConnectionsForWS)
	prometheus.MustRegister(TotalTransmitBytesOnWS)
	prometheus.MustRegister(TotalTransmitErrorBytesOnWS)
	prometheus.MustRegister(TotalReceiveBytesOnWS)
	prometheus.MustRegister(TotalAddPeerAttempt)
	prometheus.MustRegister(TotalPeerConnected)
	prometheus.MustRegister(TotalPeerDisConnected)
}

func init() {
	if os.Getenv(metricsEnv) == "true" {
		Register()
	}
}

func IncSMTotalAddWS(clientKey string, peer bool) {
	var peerStr string
	if peer {
		peerStr = "true"
	} else {
		peerStr = "false"
	}
	if prometheusMetrics {
		TotalAddWS.With(
			prometheus.Labels{
				"clientkey": clientKey,
				"peer":      peerStr,
			}).Inc()
	}
}

func IncSMTotalRemoveWS(clientKey string, peer bool) {
	var peerStr string
	if prometheusMetrics {
		if peer {
			peerStr = "true"
		} else {
			peerStr = "false"
		}
		TotalRemoveWS.With(
			prometheus.Labels{
				"clientkey": clientKey,
				"peer":      peerStr,
			}).Inc()
	}
}

func AddSMTotalTransmitErrorBytesOnWS(clientKey string, size float64) {
	if prometheusMetrics {
		TotalTransmitErrorBytesOnWS.With(
			prometheus.Labels{
				"clientkey": clientKey,
			}).Add(size)
	}
}

func AddSMTotalTransmitBytesOnWS(clientKey string, size float64) {
	// + size
	if prometheusMetrics {
		TotalTransmitBytesOnWS.With(
			prometheus.Labels{
				"clientkey": clientKey,
			}).Add(size)
	}
}

func AddSMTotalReceiveBytesOnWS(clientKey string, size float64) {
	if prometheusMetrics {
		TotalReceiveBytesOnWS.With(
			prometheus.Labels{
				"clientkey": clientKey,
			}).Add(size)
	}
}

func IncSMTotalAddConnectionsForWS(clientKey, proto, addr string) {

	// inc 请求数加1
	if prometheusMetrics {
		TotalAddConnectionsForWS.With(
			prometheus.Labels{
				"clientkey": clientKey,
				"proto":     proto,
				"addr":      addr,
			}).Inc()
	}
}

func IncSMTotalRemoveConnectionsForWS(clientKey, proto, addr string) {
	if prometheusMetrics {
		TotalRemoveConnectionsForWS.With(
			prometheus.Labels{
				"clientkey": clientKey,
				"proto":     proto,
				"addr":      addr,
			}).Inc()
	}
}

func IncSMTotalAddPeerAttempt(peer string) {
	if prometheusMetrics {
		TotalAddPeerAttempt.With(
			prometheus.Labels{
				"peer": peer,
			}).Inc()
	}
}

func IncSMTotalPeerConnected(peer string) {
	if prometheusMetrics {
		TotalPeerConnected.With(
			prometheus.Labels{
				"peer": peer,
			}).Inc()
	}
}

func IncSMTotalPeerDisConnected(peer string) {
	if prometheusMetrics {
		TotalPeerDisConnected.With(
			prometheus.Labels{
				"peer": peer,
			}).Inc()

	}
}
