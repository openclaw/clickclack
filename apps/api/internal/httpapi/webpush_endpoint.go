//go:build !clickclack_e2e_unsafe_callbacks

package httpapi

import (
	"errors"
	"net/netip"
	"strings"
)

// validatePushEndpointHost keeps stored subscriptions pointed at real push
// services. The delivery worker posts to whatever is stored, so registration
// applies the policy outgoing callbacks already use: HTTPS only, and never an
// address inside the deployment's own network. A hostname that resolves into
// private space is refused later by the callback dialer, which is the check
// that cannot be outrun by a changing DNS answer.
func validatePushEndpointHost(scheme, hostname string) error {
	if scheme != "https" {
		return errors.New("endpoint must be an https URL")
	}
	if address, err := netip.ParseAddr(strings.Trim(hostname, "[]")); err == nil {
		if !isPublicCallbackAddr(address) {
			return errors.New("endpoint must not point at a private address")
		}
		return nil
	}
	lowered := strings.ToLower(hostname)
	if !strings.Contains(lowered, ".") || lowered == "localhost" || strings.HasSuffix(lowered, ".localhost") {
		return errors.New("endpoint must be a public push service host")
	}
	return nil
}
