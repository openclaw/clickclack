//go:build !clickclack_e2e_unsafe_callbacks

package httpapi

import (
	"net"
	"net/http"
)

func newCallbackHTTPClient() *http.Client {
	resolver := net.DefaultResolver
	dialer := &net.Dialer{Timeout: callbackTimeout}
	policyDialer := &callbackDialer{
		lookupNetIP: resolver.LookupNetIP,
		dialContext: dialer.DialContext,
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = policyDialer.DialContext
	return &http.Client{
		Transport: transport,
		Timeout:   callbackTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}
