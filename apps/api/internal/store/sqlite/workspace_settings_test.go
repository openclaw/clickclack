package sqlite

import (
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store/workspacetest"
)

func TestUpdateWorkspaceKeepsReservedSlugEditable(t *testing.T) {
	t.Parallel()
	workspacetest.ReservedSlugUpdates(t, newTestStore(t))
}
