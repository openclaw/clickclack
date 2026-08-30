// Package identitysynctest proves the same import contract against each SQL backend.
package identitysynctest

import (
	"context"
	"reflect"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

type Store interface {
	store.Store
	SyncIdentities(context.Context, store.IdentitySyncInput) (store.IdentitySyncReport, error)
}

func Run(t *testing.T, open func(*testing.T) Store) {
	ctx := context.Background()
	const source = "https://control.example.com"
	t.Run("preserves authors and follows durable identity", func(t *testing.T) {
		st := open(t)
		owner, err := st.EnsureBootstrap(ctx, "Original", "owner@example.com")
		if err != nil {
			t.Fatal(err)
		}
		workspaces, err := st.ListWorkspaces(ctx, owner.ID)
		if err != nil {
			t.Fatal(err)
		}
		channels, err := st.ListChannels(ctx, workspaces[0].ID, owner.ID)
		if err != nil {
			t.Fatal(err)
		}
		message, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channels[0].ID, AuthorID: owner.ID, Body: "before identity sync"})
		if err != nil {
			t.Fatal(err)
		}
		mergedInto := "profile-owner"
		input := store.IdentitySyncInput{Source: source, Profiles: []store.IdentitySyncProfile{
			{ID: "profile-owner", DisplayName: "Owner", Emails: []string{"OWNER@EXAMPLE.COM"}},
			{ID: "profile-future", DisplayName: "Future", Emails: []string{"future@example.com"}},
			{ID: "profile-merged", MergedInto: &mergedInto},
		}}
		report, err := st.SyncIdentities(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		if report.Linked != 1 || report.Updated != 1 || report.Merged != 1 || !reflect.DeepEqual(report.Unmatched, []string{"profile-future"}) {
			t.Fatalf("unexpected initial report: %#v", report)
		}
		got, err := st.GetMessage(ctx, message.ID, owner.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.AuthorID != owner.ID || got.Author == nil || got.Author.DisplayName != "Owner" || got.Author.AvatarURL != source+"/api/users/profile-owner/avatar" {
			t.Fatalf("historical author was not hydrated from linked user: %#v", got)
		}
		input.Profiles[0].Emails = nil
		report, err = st.SyncIdentities(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		if report.Linked != 0 || report.Updated != 0 || report.Unchanged != 1 {
			t.Fatalf("sync was not idempotent without email aliases: %#v", report)
		}
		future, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "New", Email: "future@example.com"})
		if err != nil {
			t.Fatal(err)
		}
		report, err = st.SyncIdentities(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		if report.Linked != 1 || report.Updated != 1 || len(report.Unmatched) != 0 {
			t.Fatalf("rerun did not link new user: %#v", report)
		}
		if got, err := st.GetUser(ctx, future.ID); err != nil || got.AvatarURL != source+"/api/users/profile-future/avatar" {
			t.Fatalf("new user's avatar missing: %#v %v", got, err)
		}
	})
	t.Run("preserves explicit avatar overrides", func(t *testing.T) {
		st := open(t)
		user, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Original", Email: "explicit@example.com"})
		if err != nil {
			t.Fatal(err)
		}
		const avatar = "https://images.example.com/custom.png"
		_, err = st.UpdateUserProfile(ctx, store.UpdateUserProfileInput{UserID: user.ID, DisplayName: user.DisplayName, AvatarURL: avatar})
		if err != nil {
			t.Fatal(err)
		}
		input := store.IdentitySyncInput{Source: source, Profiles: []store.IdentitySyncProfile{{ID: "explicit", DisplayName: "Synced", Emails: []string{"explicit@example.com"}}}}
		if _, err := st.SyncIdentities(ctx, input); err != nil {
			t.Fatal(err)
		}
		got, err := st.GetUser(ctx, user.ID)
		if err != nil || got.DisplayName != "Synced" || got.AvatarURL != avatar {
			t.Fatalf("explicit avatar was overwritten: %#v %v", got, err)
		}
		// Clearing the explicit override restores source management on the next sync.
		_, err = st.UpdateUserProfile(ctx, store.UpdateUserProfileInput{UserID: user.ID, DisplayName: got.DisplayName})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.SyncIdentities(ctx, input); err != nil {
			t.Fatal(err)
		}
		got, err = st.GetUser(ctx, user.ID)
		if err != nil || got.AvatarURL != source+"/api/users/explicit/avatar" {
			t.Fatalf("cleared avatar did not return to source: %#v %v", got, err)
		}
	})
	for _, mode := range []string{"ambiguous email", "conflicting aliases", "reassigned identity", "same user twice"} {
		t.Run(mode+" rolls back whole import", func(t *testing.T) {
			st := open(t)
			first, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "First", Email: "first@example.com"})
			if err != nil {
				t.Fatal(err)
			}
			second, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Second", Email: "second@example.com"})
			if err != nil {
				t.Fatal(err)
			}
			input := store.IdentitySyncInput{Source: source, Profiles: []store.IdentitySyncProfile{
				{ID: "first", DisplayName: "Changed", Emails: []string{"first@example.com"}},
				{ID: "second", DisplayName: "Changed", Emails: []string{"second@example.com"}},
			}}
			switch mode {
			case "ambiguous email":
				_, err = st.UpsertIdentityUser(ctx, store.UpsertIdentityUserInput{Provider: "github", ProviderSubject: "another", Email: "second@example.com"})
			case "conflicting aliases":
				input.Profiles[0].Emails = nil
				input.Profiles[1].Emails = []string{"first@example.com", "second@example.com"}
			case "reassigned identity":
				_, err = st.SyncIdentities(ctx, store.IdentitySyncInput{Source: source, Profiles: []store.IdentitySyncProfile{{ID: "original-second", Emails: []string{"second@example.com"}}}})
			case "same user twice":
				_, err = st.SyncIdentities(ctx, store.IdentitySyncInput{Source: source, Profiles: []store.IdentitySyncProfile{{ID: "first", Emails: []string{"first@example.com"}}}})
				input.Profiles[0].Emails = nil
				input.Profiles[1].Emails = []string{"first@example.com"}
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := st.SyncIdentities(ctx, input); err == nil {
				t.Fatal("expected conflicting import to fail")
			}
			for _, user := range []store.User{first, second} {
				got, err := st.GetUser(ctx, user.ID)
				if err != nil || got.DisplayName != user.DisplayName {
					t.Fatalf("failed import changed a user: %#v %v", got, err)
				}
			}
			if mode != "same user twice" {
				report, err := st.SyncIdentities(ctx, store.IdentitySyncInput{Source: source, Profiles: []store.IdentitySyncProfile{{ID: "first"}}})
				if err != nil || !reflect.DeepEqual(report.Unmatched, []string{"first"}) {
					t.Fatalf("failed import left an identity link: %#v %v", report, err)
				}
			}
		})
	}
}
