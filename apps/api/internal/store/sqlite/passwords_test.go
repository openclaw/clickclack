package sqlite

import (
	"database/sql"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/passwordtest"
)

func TestPasswordStoreContract(t *testing.T) {
	passwordtest.Run(t, func(t *testing.T) (store.Store, *sql.DB) {
		st := newTestStore(t)
		return st, st.db
	})
}
