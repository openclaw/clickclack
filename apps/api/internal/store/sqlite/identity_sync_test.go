package sqlite

import (
	"github.com/openclaw/clickclack/apps/api/internal/store/identitysynctest"
	"testing"
)

func TestIdentitySync(t *testing.T) {
	identitysynctest.Run(t, func(t *testing.T) identitysynctest.Store {
		return newTestStore(t)
	})
}
