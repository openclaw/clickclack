//go:build clickclack_e2e_unsafe_callbacks

package httpapi

import "errors"

// validatePushEndpointHost accepts a loopback fake push service in the
// end-to-end build, the same relaxation newCallbackHTTPClient makes for
// callbacks. The production build keeps public HTTPS hosts only.
func validatePushEndpointHost(scheme, _ string) error {
	if scheme != "https" && scheme != "http" {
		return errors.New("endpoint must be an http or https URL")
	}
	return nil
}
