package postgres

import (
	"context"
	"github.com/openclaw/clickclack/apps/api/internal/store/storetest"
	"os"
	"testing"
)

func TestSidebarPreferences(t *testing.T) {
	dsn := os.Getenv("CLICKCLACK_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set CLICKCLACK_POSTGRES_TEST_DSN to run Postgres integration smoke")
	}
	st, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	storetest.SidebarPreferences(t, st)
}
