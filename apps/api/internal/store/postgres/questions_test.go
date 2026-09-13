package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store/questiontest"
)

func newMigratedQuestionStore(t *testing.T) *Store {
	t.Helper()
	st := newIsolatedPostgresTestStore(t)
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestQuestionLifecycle(t *testing.T) {
	questiontest.QuestionLifecycle(t, newMigratedQuestionStore(t))
}

func TestQuestionSkipReopenAndExternalAnswers(t *testing.T) {
	questiontest.QuestionSkipReopenAndExternalAnswers(t, newMigratedQuestionStore(t))
}

func TestQuestionExpiryAndAccess(t *testing.T) {
	st := newMigratedQuestionStore(t)
	questiontest.QuestionExpiryAndAccess(t, st, questiontest.Hooks{
		ExpireQuestion: func(t *testing.T, messageID string) {
			t.Helper()
			past := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)
			if _, err := st.db.ExecContext(context.Background(), `UPDATE message_questions SET expires_at = $1 WHERE message_id = $2`, past, messageID); err != nil {
				t.Fatal(err)
			}
		},
	})
}

func TestQuestionConcurrentAnswers(t *testing.T) {
	questiontest.QuestionConcurrentAnswers(t, newMigratedQuestionStore(t))
}

func TestQuestionReplayAndVersionGuards(t *testing.T) {
	questiontest.QuestionReplayAndVersionGuards(t, newMigratedQuestionStore(t))
}
