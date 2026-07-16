package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestPostgresBotSetupCodes(t *testing.T) {
	dsn := os.Getenv("CLICKCLACK_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set CLICKCLACK_POSTGRES_TEST_DSN to run Postgres integration smoke")
	}
	ctx := context.Background()
	st, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	suffix := time.Now().UTC().Format("20060102150405.000000000")
	owner, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Setup Code Owner", Email: "pg-setup-owner-" + suffix + "@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Setup Codes " + suffix}, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Setup Code Member", Email: "pg-setup-member-" + suffix + "@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, member.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	bot, _, err := st.CreateBot(ctx, store.CreateBotInput{WorkspaceID: workspace.ID, DisplayName: "Setup Code Bot", CreatedBy: owner.ID})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := st.CreateBotSetupCode(ctx, store.CreateBotSetupCodeInput{
		WorkspaceID: workspace.ID,
		BotUserID:   bot.ID,
		CreatedBy:   member.ID,
	}); !errors.Is(err, store.ErrNotWorkspaceManager) {
		t.Fatalf("expected member mint to require manager, got %v", err)
	}

	setup, err := st.CreateBotSetupCode(ctx, store.CreateBotSetupCodeInput{
		WorkspaceID: workspace.ID,
		BotUserID:   bot.ID,
		Name:        "gateway",
		Scopes:      []string{"messages:write"},
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(setup.Code) != 14 || strings.Count(setup.Code, "-") != 2 {
		t.Fatalf("expected XXXX-XXXX-XXXX code, got %q", setup.Code)
	}

	claim, err := st.ClaimBotSetupCode(ctx, strings.ToLower(setup.Code))
	if err != nil {
		t.Fatal(err)
	}
	if claim.BotToken.Token == "" || claim.Bot.ID != bot.ID || claim.Workspace.ID != workspace.ID {
		t.Fatalf("unexpected claim result: %#v", claim)
	}
	auth, err := st.GetBotTokenAuth(ctx, claim.BotToken.Token)
	if err != nil {
		t.Fatal(err)
	}
	if auth.User.ID != bot.ID || auth.WorkspaceID != workspace.ID {
		t.Fatalf("expected minted token to authenticate, got %#v", auth)
	}

	if _, err := st.ClaimBotSetupCode(ctx, setup.Code); !errors.Is(err, store.ErrSetupCodeInvalid) {
		t.Fatalf("expected second claim to fail uniformly, got %v", err)
	}

	expiredCode, err := st.CreateBotSetupCode(ctx, store.CreateBotSetupCodeInput{WorkspaceID: workspace.ID, BotUserID: bot.ID, Name: "expiring", CreatedBy: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	expired := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	if _, err := st.db.ExecContext(ctx, `UPDATE bot_setup_codes SET expires_at = $1 WHERE id = $2`, expired, expiredCode.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ClaimBotSetupCode(ctx, expiredCode.Code); !errors.Is(err, store.ErrSetupCodeInvalid) {
		t.Fatalf("expected expired claim to fail uniformly, got %v", err)
	}

	pending, err := st.CreateBotSetupCode(ctx, store.CreateBotSetupCodeInput{WorkspaceID: workspace.ID, BotUserID: bot.ID, Name: "pending", CreatedBy: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RemoveBotFromWorkspace(ctx, workspace.ID, bot.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ClaimBotSetupCode(ctx, pending.Code); !errors.Is(err, store.ErrSetupCodeInvalid) {
		t.Fatalf("expected claim after removal to fail uniformly, got %v", err)
	}
}
