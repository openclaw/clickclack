package sqlite

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func setupCodeFixture(t *testing.T) (*Store, store.Workspace, store.User, store.User) {
	t.Helper()
	ctx := context.Background()
	st := newTestStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Owner", "owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaces[0]
	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Member", Email: "member-setup-codes@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, member.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	return st, workspace, owner, member
}

func TestBotSetupCodeMintAndClaim(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st, workspace, owner, member := setupCodeFixture(t)

	bot, _, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		DisplayName: "Service Bot",
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := st.CreateBotSetupCode(ctx, store.CreateBotSetupCodeInput{
		WorkspaceID: workspace.ID,
		BotUserID:   bot.ID,
		Name:        "gateway",
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
	if setup.ExpiresAt == "" || setupCodePlaintextAtRest(t, st, setup.Code) {
		t.Fatalf("unexpected setup code state: %#v", setup)
	}

	// No new token exists until claim (CreateBot minted the initial one).
	tokens, err := st.ListBotTokensForWorkspace(ctx, workspace.ID, bot.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	baseline := len(tokens)

	// Claim is normalization-tolerant: lowercase without separators.
	claim, err := st.ClaimBotSetupCode(ctx, strings.ToLower(strings.ReplaceAll(setup.Code, "-", "")))
	if err != nil {
		t.Fatal(err)
	}
	tokens, err = st.ListBotTokensForWorkspace(ctx, workspace.ID, bot.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != baseline+1 {
		t.Fatalf("expected claim to mint exactly one token, got %d -> %d", baseline, len(tokens))
	}
	if claim.BotToken.Token == "" || !strings.HasPrefix(claim.BotToken.Token, "ccb_") {
		t.Fatalf("expected plaintext token in claim, got %#v", claim.BotToken)
	}
	if claim.Bot.ID != bot.ID || claim.Workspace.ID != workspace.ID {
		t.Fatalf("unexpected claim context: %#v", claim)
	}
	if claim.Defaults.DefaultTo != "channel:general" {
		t.Fatalf("expected general channel suggestion, got %#v", claim.Defaults)
	}
	if claim.BotToken.Name != "gateway" || len(claim.BotToken.Scopes) != 1 || claim.BotToken.Scopes[0] != "messages:write" {
		t.Fatalf("expected captured name/scopes, got %#v", claim.BotToken)
	}
	auth, err := st.GetBotTokenAuth(ctx, claim.BotToken.Token)
	if err != nil {
		t.Fatal(err)
	}
	if auth.User.ID != bot.ID || auth.WorkspaceID != workspace.ID {
		t.Fatalf("expected minted token to authenticate, got %#v", auth)
	}

	// Single use.
	if _, err := st.ClaimBotSetupCode(ctx, setup.Code); !errors.Is(err, store.ErrSetupCodeInvalid) {
		t.Fatalf("expected second claim to fail uniformly, got %v", err)
	}
}

func TestBotSetupCodeConcurrentClaimMintsOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st, workspace, owner, _ := setupCodeFixture(t)
	bot, _, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		DisplayName: "Concurrent Claim Bot",
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	setup, err := st.CreateBotSetupCode(ctx, store.CreateBotSetupCodeInput{
		WorkspaceID: workspace.ID,
		BotUserID:   bot.ID,
		Name:        "concurrent",
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	before, err := st.ListBotTokensForWorkspace(ctx, workspace.ID, bot.ID, owner.ID)
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
			_, claimErr := st.ClaimBotSetupCode(ctx, setup.Code)
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
	after, err := st.ListBotTokensForWorkspace(ctx, workspace.ID, bot.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before)+1 {
		t.Fatalf("expected exactly one minted token, got %d -> %d", len(before), len(after))
	}
}

// setupCodePlaintextAtRest reports whether the plaintext code leaked into
// the code_hash column.
func setupCodePlaintextAtRest(t *testing.T, st *Store, code string) bool {
	t.Helper()
	var count int
	normalized := strings.ReplaceAll(code, "-", "")
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM bot_setup_codes WHERE code_hash = ? OR code_hash = ?`, code, normalized).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count != 0
}

func TestBotSetupCodeExpiryAndInvalidInputs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st, workspace, owner, _ := setupCodeFixture(t)

	bot, _, err := st.CreateBot(ctx, store.CreateBotInput{WorkspaceID: workspace.ID, DisplayName: "Expiring Bot", CreatedBy: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	setup, err := st.CreateBotSetupCode(ctx, store.CreateBotSetupCodeInput{
		WorkspaceID: workspace.ID,
		BotUserID:   bot.ID,
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if setup.TokenName != "default" {
		t.Fatalf("expected default token name, got %q", setup.TokenName)
	}
	expired := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	if _, err := st.db.ExecContext(ctx, `UPDATE bot_setup_codes SET expires_at = ? WHERE id = ?`, expired, setup.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ClaimBotSetupCode(ctx, setup.Code); !errors.Is(err, store.ErrSetupCodeInvalid) {
		t.Fatalf("expected expired claim to fail uniformly, got %v", err)
	}

	for _, invalid := range []string{"", "short", "!!!!-!!!!-!!!!", strings.Repeat("A", 13)} {
		if _, err := st.ClaimBotSetupCode(ctx, invalid); !errors.Is(err, store.ErrSetupCodeInvalid) {
			t.Fatalf("expected invalid code %q to fail uniformly, got %v", invalid, err)
		}
	}

	// A fresh mint lazily purges the expired row.
	if _, err := st.CreateBotSetupCode(ctx, store.CreateBotSetupCodeInput{WorkspaceID: workspace.ID, BotUserID: bot.ID, Name: "other", CreatedBy: owner.ID}); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := st.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM bot_setup_codes WHERE id = ?`, setup.ID).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatal("expected expired setup code to be purged on next mint")
	}
}

func TestBotSetupCodeRemintReplacesPending(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st, workspace, owner, _ := setupCodeFixture(t)

	bot, _, err := st.CreateBot(ctx, store.CreateBotInput{WorkspaceID: workspace.ID, DisplayName: "Remint Bot", CreatedBy: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	first, err := st.CreateBotSetupCode(ctx, store.CreateBotSetupCodeInput{WorkspaceID: workspace.ID, BotUserID: bot.ID, Name: "gateway", CreatedBy: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.CreateBotSetupCode(ctx, store.CreateBotSetupCodeInput{WorkspaceID: workspace.ID, BotUserID: bot.ID, Name: "gateway", CreatedBy: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ClaimBotSetupCode(ctx, first.Code); !errors.Is(err, store.ErrSetupCodeInvalid) {
		t.Fatalf("expected replaced code to be unclaimable, got %v", err)
	}
	if _, err := st.ClaimBotSetupCode(ctx, second.Code); err != nil {
		t.Fatalf("expected replacement code to claim, got %v", err)
	}
}

func TestBotSetupCodeLifecycleCascades(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st, workspace, owner, _ := setupCodeFixture(t)

	removed, _, err := st.CreateBot(ctx, store.CreateBotInput{WorkspaceID: workspace.ID, DisplayName: "Removed Bot", CreatedBy: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	removedCode, err := st.CreateBotSetupCode(ctx, store.CreateBotSetupCodeInput{WorkspaceID: workspace.ID, BotUserID: removed.ID, CreatedBy: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RemoveBotFromWorkspace(ctx, workspace.ID, removed.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ClaimBotSetupCode(ctx, removedCode.Code); !errors.Is(err, store.ErrSetupCodeInvalid) {
		t.Fatalf("expected claim after removal to fail uniformly, got %v", err)
	}
	var count int
	if err := st.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM bot_setup_codes WHERE bot_user_id = ?`, removed.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("expected pending codes to be deleted on workspace removal")
	}

	deleted, _, err := st.CreateBot(ctx, store.CreateBotInput{WorkspaceID: workspace.ID, DisplayName: "Deleted Bot", CreatedBy: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	deletedCode, err := st.CreateBotSetupCode(ctx, store.CreateBotSetupCodeInput{WorkspaceID: workspace.ID, BotUserID: deleted.ID, CreatedBy: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.DeleteBot(ctx, deleted.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ClaimBotSetupCode(ctx, deletedCode.Code); !errors.Is(err, store.ErrSetupCodeInvalid) {
		t.Fatalf("expected claim after bot deletion to fail uniformly, got %v", err)
	}
	if err := st.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM bot_setup_codes WHERE bot_user_id = ?`, deleted.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("expected pending codes to be deleted with the bot")
	}
}

func TestBotSetupCodeUserOwnedBotAuthorization(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st, workspace, owner, member := setupCodeFixture(t)

	userBot, _, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		OwnerUserID: member.ID,
		DisplayName: "Member Bot",
		CreatedBy:   member.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateBotSetupCode(ctx, store.CreateBotSetupCodeInput{
		WorkspaceID: workspace.ID,
		BotUserID:   userBot.ID,
		CreatedBy:   owner.ID,
	}); !errors.Is(err, store.ErrBotOwnerRequired) {
		t.Fatalf("expected non-owner mint on user-owned bot to fail, got %v", err)
	}
	setup, err := st.CreateBotSetupCode(ctx, store.CreateBotSetupCodeInput{
		WorkspaceID: workspace.ID,
		BotUserID:   userBot.ID,
		CreatedBy:   member.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := st.ClaimBotSetupCode(ctx, setup.Code)
	if err != nil {
		t.Fatal(err)
	}
	if claim.BotToken.OwnerUserID != member.ID {
		t.Fatalf("expected owner to carry into minted token, got %#v", claim.BotToken)
	}
}
