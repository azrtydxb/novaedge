package acme

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// certificateExpirySeconds tracks time until certificate expiry
	certificateExpirySeconds = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "novaedge",
			Subsystem: "certificate",
			Name:      "expiry_seconds",
			Help:      "Seconds until certificate expires",
		},
		[]string{"domain", "issuer"},
	)

	// renewalsTotal counts certificate renewals
	renewalsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "novaedge",
			Subsystem: "certificate",
			Name:      "renewals_total",
			Help:      "Total certificate renewals",
		},
		[]string{"domain", "status"},
	)

	// acmeChallengesTotal counts ACME challenges
	acmeChallengesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "novaedge",
			Subsystem: "acme",
			Name:      "challenges_total",
			Help:      "Total ACME challenges",
		},
		[]string{"type", "status"},
	)

	// certificateRequestDuration tracks time to obtain certificates
	certificateRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "novaedge",
			Subsystem: "acme",
			Name:      "request_duration_seconds",
			Help:      "Duration of certificate requests",
			Buckets:   prometheus.ExponentialBuckets(1, 2, 10), // 1s to ~17min
		},
		[]string{"status"},
	)

	// certificatesManaged tracks number of managed certificates
	certificatesManaged = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "novaedge",
			Subsystem: "certificate",
			Name:      "managed_total",
			Help:      "Total number of managed certificates",
		},
	)
)

// UpdateCertificateMetrics updates metrics for a certificate.
func UpdateCertificateMetrics(cert *Certificate) {
	if cert == nil || len(cert.Domains) == 0 {
		return
	}

	domain := cert.Domains[0]
	expirySeconds := cert.ExpiresIn().Seconds()

	certificateExpirySeconds.WithLabelValues(domain, cert.Issuer).Set(expirySeconds)
}

// SetManagedCertificatesCount updates the managed certificates count.
func SetManagedCertificatesCount(count int) {
	certificatesManaged.Set(float64(count))
}

// RecordChallengeResult records an ACME challenge result.
func RecordChallengeResult(challengeType, status string) {
	acmeChallengesTotal.WithLabelValues(challengeType, status).Inc()
}

// RecordRequestDuration records certificate request duration.
func RecordRequestDuration(seconds float64, status string) {
	certificateRequestDuration.WithLabelValues(status).Observe(seconds)
}
