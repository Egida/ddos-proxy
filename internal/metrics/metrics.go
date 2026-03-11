package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// DroppedRequests counts the number of dropped/blocked requests.
	DroppedRequests = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ddos_proxy_dropped_requests_total",
			Help: "The total number of dropped requests",
		},
		[]string{"reason"},
	)

	// ChallengedRequests counts the number of requests served a challenge.
	ChallengedRequests = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "ddos_proxy_challenged_requests_total",
			Help: "The total number of challenged requests",
		},
	)

	// AllowedRequests counts the number of allowed requests passed to the backend.
	AllowedRequests = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ddos_proxy_allowed_requests_total",
			Help: "The total number of allowed requests",
		},
		[]string{"reason"},
	)
)
