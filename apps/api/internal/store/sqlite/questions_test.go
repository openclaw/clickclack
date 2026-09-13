package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store/questiontest"
)

func TestQuestionLifecycle(t *testing.T) {
	t.Parallel()
	questiontest.QuestionLifecycle(t, newTestStore(t))
}

func TestQuestionSkipReopenAndExternalAnswers(t *testing.T) {
	t.Parallel()
	questiontest.QuestionSkipReopenAndExternalAnswers(t, newTestStore(t))
}

func TestQuestionExpiryAndAccess(t *testing.T) {
	t.Parallel()
	st := newTestStore(t)
	questiontest.QuestionExpiryAndAccess(t, st, questiontest.Hooks{
		ExpireQuestion: func(t *testing.T, messageID string) {
			t.Helper()
			past := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)
			if _, err := st.db.ExecContext(context.Background(), `UPDATE message_questions SET expires_at = ? WHERE message_id = ?`, past, messageID); err != nil {
				t.Fatal(err)
			}
		},
	})
}

func TestQuestionConcurrentAnswers(t *testing.T) {
	t.Parallel()
	questiontest.QuestionConcurrentAnswers(t, newTestStore(t))
}

func TestQuestionReplayAndVersionGuards(t *testing.T) {
	t.Parallel()
	questiontest.QuestionReplayAndVersionGuards(t, newTestStore(t))
}
