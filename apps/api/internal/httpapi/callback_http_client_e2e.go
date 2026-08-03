//go:build clickclack_e2e_unsafe_callbacks

package httpapi

import (
	"log"
	"net/http"
)

func newCallbackHTTPClient() *http.Client {
	log.Print("WARNING: E2E build allows loopback callback destinations")
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &http.Client{
		Transport: transport,
		Timeout:   callbackTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}
