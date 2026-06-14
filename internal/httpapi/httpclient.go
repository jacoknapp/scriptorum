package httpapi

import (
	"crypto/tls"
	"net/http"
	"time"
)

// outboundTLSInsecure reports whether outbound TLS verification should be
// skipped. Self-hosted deployments often use self-signed certificates on their
// Readarr/ntfy/SMTP/OIDC backends, so this is controlled by the global
// insecure_skip_verify config flag.
func (s *Server) outboundTLSInsecure() bool {
	if cfg := s.settings.Get(); cfg != nil {
		return cfg.InsecureSkipVerify
	}
	return false
}

// outboundHTTPClient returns an http.Client for talking to self-hosted backends,
// honoring the global insecure_skip_verify flag.
func (s *Server) outboundHTTPClient(timeout time.Duration) *http.Client {
	client := &http.Client{Timeout: timeout}
	if s.outboundTLSInsecure() {
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}
	return client
}
