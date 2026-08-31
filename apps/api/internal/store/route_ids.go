package store

import (
	"crypto/rand"
	"encoding/base32"
)

var routeIDEncoding = base32.NewEncoding("0123456789ABCDEFGHJKMNPQRSTVWXYZ").WithPadding(base32.NoPadding)

// NewRouteID returns a byte prefix followed by 16 base32 characters from 80 random bits.
func NewRouteID(prefix byte) (string, error) {
	var raw [10]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	var out [17]byte
	out[0] = prefix
	routeIDEncoding.Encode(out[1:], raw[:])
	return string(out[:]), nil
}
