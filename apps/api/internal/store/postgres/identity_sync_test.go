package postgres

import (
	"context"
	"github.com/openclaw/clickclack/apps/api/internal/store/identitysynctest"
	"testing"
)

func TestIdentitySync(t *testing.T) {
	identitysynctest.Run(t, func(t *testing.T) identitysynctest.Store {
		st := newIsolatedPostgresTestStore(t)
		if err := st.Migrate(context.Background()); err != nil {
			t.Fatal(err)
		}
		return st
	})
}
