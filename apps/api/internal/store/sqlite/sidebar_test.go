package sqlite

import (
	"github.com/openclaw/clickclack/apps/api/internal/store/storetest"
	"testing"
)

func TestSidebarPreferences(t *testing.T) {
	t.Parallel()
	storetest.SidebarPreferences(t, newTestStore(t))
}
