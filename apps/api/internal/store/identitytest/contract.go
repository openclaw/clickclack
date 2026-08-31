package identitytest

import (
	"context"
	"sync"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func Run(t *testing.T, open func(*testing.T) store.Store) {
	t.Helper()
	for _, tc := range []struct {
		name string
		run  func(*testing.T, store.Store)
	}{
		{"ProviderSubjectAndProfilePolicy", providerSubjectAndProfilePolicy},
		{"ConcurrentProviderAvatarWinsLateEmailFallback", concurrentProviderAvatarWinsLateEmailFallback},
		{"ConcurrentProfileClearRestoresLateEmailFallback", concurrentProfileClearRestoresLateEmailFallback},
	} {
		t.Run(tc.name, func(t *testing.T) { tc.run(t, open(t)) })
	}
}

func providerSubjectAndProfilePolicy(t *testing.T, st store.Store) {
	ctx := context.Background()
	const email = "shared@example.com"
	local, err := st.CreateUser(ctx, store.CreateUserInput{Email: email, DisplayName: "Local"})
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{local.ID: true}
	var githubID string
	for _, input := range []store.UpsertIdentityUserInput{
		{Provider: "github", ProviderSubject: "first", Email: email},
		{Provider: "github", ProviderSubject: "second", Email: email},
		{Provider: "other", ProviderSubject: "first", Email: email},
	} {
		user, err := st.UpsertIdentityUser(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		if ids[user.ID] {
			t.Fatalf("linked a different provider/subject by email: %#v", input)
		}
		ids[user.ID] = true
		if input.Provider == "github" && input.ProviderSubject == "first" {
			githubID = user.ID
		}
	}
	input := store.UpsertIdentityUserInput{Provider: " github ", ProviderSubject: " first ", Email: "changed@example.com", AvatarURL: "https://example.com/provider.png", DisplayName: "Provider rename"}
	user, err := st.UpsertIdentityUser(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if user.ID != githubID || user.DisplayName != email || user.AvatarURL != input.AvatarURL {
		t.Fatalf("provider lookup or fallback upgrade changed: %#v", user)
	}
	custom := "https://example.com/custom.png"
	settings := store.NotificationSettings{PushoverEnabled: true, PushoverUserKey: "123456789012345678901234567890"}
	if _, err := st.UpdateCurrentUser(ctx, store.UpdateCurrentUserInput{UserID: user.ID, AvatarURL: &custom, NotificationSettings: &settings}); err != nil {
		t.Fatal(err)
	}
	again, err := st.UpsertIdentityUser(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != user.ID || again.AvatarURL != custom || again.NotificationSettings == nil || *again.NotificationSettings != settings {
		t.Fatalf("sign-in lost explicit avatar or notification settings: %#v", again)
	}
	empty := ""
	cleared, err := st.UpdateCurrentUser(ctx, store.UpdateCurrentUserInput{UserID: user.ID, AvatarURL: &empty})
	if err != nil {
		t.Fatal(err)
	}
	if want := store.ResolveAvatarURL("", email); cleared.User.AvatarURL != want {
		t.Fatalf("sign-in replaced a nonempty identity email: avatar = %q, want %q", cleared.User.AvatarURL, want)
	}
}

func concurrentProviderAvatarWinsLateEmailFallback(t *testing.T, st store.Store) {
	ctx := context.Background()
	identity := store.UpsertIdentityUserInput{
		Provider:        "github",
		ProviderSubject: "concurrent-avatar",
		DisplayName:     "Concurrent Avatar",
	}
	user, err := st.UpsertIdentityUser(ctx, identity)
	if err != nil {
		t.Fatal(err)
	}
	if user.AvatarURL != "" {
		t.Fatalf("expected initial blank avatar, got %#v", user)
	}

	start := make(chan struct{})
	errors := make(chan error, 2)
	var group sync.WaitGroup
	for _, input := range []store.UpsertIdentityUserInput{
		{
			Provider:        identity.Provider,
			ProviderSubject: identity.ProviderSubject,
			Email:           "concurrent-avatar@example.com",
		},
		{
			Provider:        identity.Provider,
			ProviderSubject: identity.ProviderSubject,
			AvatarURL:       "https://example.com/provider-concurrent.png",
		},
	} {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			_, err := st.UpsertIdentityUser(ctx, input)
			errors <- err
		}()
	}
	close(start)
	group.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	user, err = st.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if user.AvatarURL != "https://example.com/provider-concurrent.png" {
		t.Fatalf("expected provider avatar to win concurrent late-email fallback, got %#v", user)
	}
}

func concurrentProfileClearRestoresLateEmailFallback(t *testing.T, st store.Store) {
	ctx := context.Background()
	identity := store.UpsertIdentityUserInput{
		Provider:        "github",
		ProviderSubject: "concurrent-clear",
		DisplayName:     "Concurrent Clear",
		AvatarURL:       "https://example.com/custom-before-clear.png",
	}
	user, err := st.UpsertIdentityUser(ctx, identity)
	if err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	errors := make(chan error, 2)
	var group sync.WaitGroup
	group.Add(2)
	go func() {
		defer group.Done()
		<-start
		_, err := st.UpsertIdentityUser(ctx, store.UpsertIdentityUserInput{
			Provider:        identity.Provider,
			ProviderSubject: identity.ProviderSubject,
			Email:           "concurrent-clear@example.com",
		})
		errors <- err
	}()
	go func() {
		defer group.Done()
		<-start
		empty := ""
		_, err := st.UpdateCurrentUser(ctx, store.UpdateCurrentUserInput{
			UserID:      user.ID,
			DisplayName: &user.DisplayName,
			Handle:      &empty,
			AvatarURL:   &empty,
		})
		errors <- err
	}()
	close(start)
	group.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	user, err = st.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if want := store.ResolveAvatarURL("", "concurrent-clear@example.com"); user.AvatarURL != want {
		t.Fatalf("expected late-email Gravatar %q after concurrent clear, got %#v", want, user)
	}
}
