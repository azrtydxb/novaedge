/*
Copyright 2024 NovaEdge Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package server

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"

	"go.uber.org/zap"

	pb "github.com/piwi3910/novaedge/internal/proto/gen"
)

var (
	errFailedToParseClientCACertificateBundle   = errors.New("failed to parse client CA certificate bundle")
	errMTLSModeRequireNeedsACACertificateBundle = errors.New("mTLS mode 'require' needs a CA certificate bundle but none was provided")
)

// parseClientAuth converts a ClientAuthConfig mode string to tls.ClientAuthType
func parseClientAuth(mode string) tls.ClientAuthType {
	switch strings.ToLower(mode) {
	case "require":
		return tls.RequireAndVerifyClientCert
	case "optional":
		return tls.VerifyClientCertIfGiven
	default:
		return tls.NoClientCert
	}
}

// applyClientAuthConfig applies mTLS client authentication settings to a tls.Config
func (s *HTTPServer) applyClientAuthConfig(tlsConfig *tls.Config, clientAuth *pb.ClientAuthConfig) error {
	if clientAuth == nil || clientAuth.Mode == "" || clientAuth.Mode == "none" {
		return nil
	}

	tlsConfig.ClientAuth = parseClientAuth(clientAuth.Mode)

	// Load CA certificate pool for client cert verification
	if len(clientAuth.CaCert) > 0 {
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(clientAuth.CaCert) {
			return errFailedToParseClientCACertificateBundle
		}
		tlsConfig.ClientCAs = caCertPool

		s.logger.Info("mTLS client authentication configured",
			zap.String("mode", clientAuth.Mode),
			zap.Int("ca_certs_loaded", len(clientAuth.CaCert)),
		)
	} else if clientAuth.Mode == "require" {
		return errMTLSModeRequireNeedsACACertificateBundle
	}

	return nil
}

// injectClientCertHeaders extracts client certificate information and injects
// it as HTTP headers for downstream handlers to use.
func injectClientCertHeaders(r *http.Request) {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return
	}

	// Strip existing client cert headers to prevent spoofing
	for key := range r.Header {
		if strings.HasPrefix(key, "X-Client-Cert-") {
			r.Header.Del(key)
		}
	}

	clientCert := r.TLS.PeerCertificates[0]

	// Inject Common Name
	if clientCert.Subject.CommonName != "" {
		r.Header.Set("X-Client-Cert-CN", clientCert.Subject.CommonName)
	}

	// Inject Subject Alternative Names (DNS)
	if len(clientCert.DNSNames) > 0 {
		r.Header.Set("X-Client-Cert-SAN-DNS", strings.Join(clientCert.DNSNames, ","))
	}

	// Inject Subject Alternative Names (Email)
	if len(clientCert.EmailAddresses) > 0 {
		r.Header.Set("X-Client-Cert-SAN-Email", strings.Join(clientCert.EmailAddresses, ","))
	}

	// Inject Subject Alternative Names (URI)
	if len(clientCert.URIs) > 0 {
		uris := make([]string, 0, len(clientCert.URIs))
		for _, u := range clientCert.URIs {
			uris = append(uris, u.String())
		}
		r.Header.Set("X-Client-Cert-SAN-URI", strings.Join(uris, ","))
	}

	// Inject certificate fingerprint (SHA-256)
	fingerprint := sha256.Sum256(clientCert.Raw)
	r.Header.Set("X-Client-Cert-Fingerprint", hex.EncodeToString(fingerprint[:]))

	// Inject serial number
	if clientCert.SerialNumber != nil {
		r.Header.Set("X-Client-Cert-Serial", clientCert.SerialNumber.String())
	}

	// Inject issuer
	r.Header.Set("X-Client-Cert-Issuer", clientCert.Issuer.String())

	// Inject subject
	r.Header.Set("X-Client-Cert-Subject", clientCert.Subject.String())
}
