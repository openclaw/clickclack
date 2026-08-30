package postgres

import (
	"context"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store/workspacetest"
)

func TestUpdateWorkspaceKeepsReservedSlugEditable(t *testing.T) {
	st := newIsolatedPostgresTestStore(t)
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	workspacetest.ReservedSlugUpdates(t, st)
}
