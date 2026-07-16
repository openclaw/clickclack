package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
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
	if _, _, err := st.CreateChannel(ctx, store.CreateChannelInput{
		WorkspaceID: workspace.ID,
		UserID:      owner.ID,
		Name:        "general",
		Kind:        "public",
	}); err != nil {
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
	if claim.Defaults.DefaultTo != "channel:general" {
		t.Fatalf("expected general channel suggestion, got %#v", claim.Defaults)
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

	concurrent, err := st.CreateBotSetupCode(ctx, store.CreateBotSetupCodeInput{
		WorkspaceID: workspace.ID,
		BotUserID:   bot.ID,
		Name:        "concurrent",
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, claimErr := st.ClaimBotSetupCode(ctx, concurrent.Code)
			results <- claimErr
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	var successes, invalid int
	for claimErr := range results {
		switch {
		case claimErr == nil:
			successes++
		case errors.Is(claimErr, store.ErrSetupCodeInvalid):
			invalid++
		default:
			t.Fatalf("unexpected concurrent claim error: %v", claimErr)
		}
	}
	if successes != 1 || invalid != 1 {
		t.Fatalf("expected one success and one uniform rejection, got success=%d invalid=%d", successes, invalid)
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

func TestPostgresBotSetupCodeClaimLocksLifecycleBeforeCodeRow(t *testing.T) {
	ctx := context.Background()
	st := newIsolatedPostgresTestStore(t)
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := st.EnsureBootstrap(ctx, "Setup Owner", "postgres-setup-lock-owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaces[0]
	bot, _, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		DisplayName: "Setup Lock Bot",
		Handle:      "postgres-setup-lock-bot",
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	setup, err := st.CreateBotSetupCode(ctx, store.CreateBotSetupCodeInput{
		WorkspaceID: workspace.ID,
		BotUserID:   bot.ID,
		Name:        "lock-order",
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	blocker := mustBeginPostgresTx(t, ctx, st.db)
	if err := lockBotLifecycleTx(ctx, blocker, bot.ID); err != nil {
		t.Fatal(err)
	}

	claimResult := make(chan error, 1)
	go func() {
		_, claimErr := st.ClaimBotSetupCode(ctx, setup.Code)
		claimResult <- claimErr
	}()
	waitForBlockedBotLifecycleOperations(t, ctx, st.db, 1)

	lockCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if _, err := st.q.WithTx(blocker).LockBotSetupCodeByHash(lockCtx, hashBotSetupCode(strings.ReplaceAll(setup.Code, "-", ""))); err != nil {
		t.Fatalf("claim locked the setup-code row before the bot lifecycle lock: %v", err)
	}
	if err := blocker.Commit(); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-claimResult:
		if err != nil {
			t.Fatalf("claim failed after lifecycle lock release: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("claim did not resume after lifecycle lock release")
	}
}

func TestPostgresRemoveBotFromWorkspaceUsesLifecycleLock(t *testing.T) {
	ctx := context.Background()
	st := newIsolatedPostgresTestStore(t)
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := st.EnsureBootstrap(ctx, "Removal Owner", "postgres-setup-remove-owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaces[0]
	bot, _, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		DisplayName: "Removal Lock Bot",
		Handle:      "postgres-setup-remove-lock-bot",
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	blocker := mustBeginPostgresTx(t, ctx, st.db)
	if err := lockBotLifecycleTx(ctx, blocker, bot.ID); err != nil {
		t.Fatal(err)
	}

	removeResult := make(chan error, 1)
	go func() {
		removeResult <- st.RemoveBotFromWorkspace(ctx, workspace.ID, bot.ID, owner.ID)
	}()
	waitForBlockedBotLifecycleOperations(t, ctx, st.db, 1)
	if err := blocker.Commit(); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-removeResult:
		if err != nil {
			t.Fatalf("bot removal failed after lifecycle lock release: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("bot removal did not resume after lifecycle lock release")
	}
}

func TestPostgresWorkspaceDeletionUsesUserBotLifecycleLock(t *testing.T) {
	ctx := context.Background()
	st := newIsolatedPostgresTestStore(t)
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := st.EnsureBootstrap(ctx, "Workspace Owner", "postgres-setup-workspace-delete-owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaces[0]
	bot, _, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		OwnerUserID: owner.ID,
		DisplayName: "Owned Setup Bot",
		Handle:      "postgres-owned-setup-delete-bot",
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	setup, err := st.CreateBotSetupCode(ctx, store.CreateBotSetupCodeInput{
		WorkspaceID: workspace.ID,
		BotUserID:   bot.ID,
		Name:        "workspace-delete",
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	blocker := mustBeginPostgresTx(t, ctx, st.db)
	if err := lockBotLifecycleTx(ctx, blocker, bot.ID); err != nil {
		t.Fatal(err)
	}

	deleteResult := make(chan error, 1)
	go func() {
		_, deleteErr := st.DeleteWorkspace(ctx, workspace.ID, owner.ID)
		deleteResult <- deleteErr
	}()
	waitForBlockedBotLifecycleOperations(t, ctx, st.db, 1)
	if err := blocker.Commit(); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-deleteResult:
		if err != nil {
			t.Fatalf("workspace deletion failed after lifecycle lock release: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("workspace deletion did not resume after lifecycle lock release")
	}
	if _, err := st.ClaimBotSetupCode(ctx, setup.Code); !errors.Is(err, store.ErrSetupCodeInvalid) {
		t.Fatalf("workspace deletion left its user-owned bot setup code claimable: %v", err)
	}
}

func TestPostgresCreateBotWithoutInitialToken(t *testing.T) {
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
	owner, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Skip Token Owner", Email: "pg-skip-owner-" + suffix + "@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Skip Token " + suffix}, owner.ID)
	if err != nil {
		t.Fatal(err)
	}

	input := store.CreateBotInput{
		WorkspaceID:      workspace.ID,
		DisplayName:      "Codeless Bot",
		CreatedBy:        owner.ID,
		SetupNonce:       "0d4e8a1c-9f27-4f6e-8c35-1a2b3c4d5e6f",
		SkipInitialToken: true,
	}
	bot, token, err := st.CreateBot(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if token.ID != "" || token.Token != "" {
		t.Fatalf("expected no initial token, got %#v", token)
	}
	replayed, replayedToken, err := st.CreateBot(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.ID != bot.ID || replayedToken.ID != "" {
		t.Fatalf("expected tokenless replay to reuse bot, got %#v / %#v", replayed, replayedToken)
	}
	conflicting := input
	conflicting.DisplayName = "Different Bot"
	if _, _, err := st.CreateBot(ctx, conflicting); !errors.Is(err, store.ErrSetupNonceConflict) {
		t.Fatalf("expected changed tokenless replay to conflict, got %v", err)
	}
	conflicting = input
	conflicting.SkipInitialToken = false
	if _, _, err := st.CreateBot(ctx, conflicting); !errors.Is(err, store.ErrSetupNonceConflict) {
		t.Fatalf("expected token mode reuse of tokenless nonce to conflict, got %v", err)
	}
	tokens, err := st.ListBotTokensForWorkspace(ctx, workspace.ID, bot.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 0 {
		t.Fatalf("expected zero tokens for bot, got %d", len(tokens))
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
	claim, err := st.ClaimBotSetupCode(ctx, setup.Code)
	if err != nil {
		t.Fatal(err)
	}
	if claim.Bot.ID != bot.ID {
		t.Fatalf("claim bot mismatch: %#v", claim.Bot)
	}
	tokens, err = st.ListBotTokensForWorkspace(ctx, workspace.ID, bot.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 1 {
		t.Fatalf("expected exactly one token after claim, got %d", len(tokens))
	}
}
