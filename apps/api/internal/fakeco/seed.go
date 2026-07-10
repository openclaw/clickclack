package fakeco

import (
	"context"
	"fmt"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

const SeedVersion = "fakeco.seed.v1"

type UserManifest struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Handle string `json:"handle"`
	Email  string `json:"email"`
}

type ChannelManifest struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Manifest struct {
	Version    string            `json:"version"`
	Workspace  store.Workspace   `json:"workspace"`
	Users      []UserManifest    `json:"users"`
	Channels   []ChannelManifest `json:"channels"`
	MessageIDs map[string]string `json:"message_ids"`
}

type userSpec struct {
	Subject string
	Name    string
	Handle  string
	Email   string
}

var seedUsers = []userSpec{
	{Subject: "alice-nguyen", Name: "Alice Nguyen", Handle: "alice", Email: "alice@fakeco.example.invalid"},
	{Subject: "ben-carter", Name: "Ben Carter", Handle: "ben", Email: "ben@fakeco.example.invalid"},
	{Subject: "casey-rivera", Name: "Casey Rivera", Handle: "casey", Email: "casey@fakeco.example.invalid"},
}

var seedChannels = []string{"general", "engineering", "incidents", "e2e-canary"}

func Seed(ctx context.Context, st store.Store) (Manifest, error) {
	users := make([]store.User, 0, len(seedUsers))
	userManifest := make([]UserManifest, 0, len(seedUsers))
	for _, spec := range seedUsers {
		user, err := st.UpsertIdentityUser(ctx, store.UpsertIdentityUserInput{
			Provider:        "fakeco",
			ProviderSubject: spec.Subject,
			Email:           spec.Email,
			DisplayName:     spec.Name,
		})
		if err != nil {
			return Manifest{}, fmt.Errorf("seed user %s: %w", spec.Subject, err)
		}
		user, err = st.UpdateUserProfile(ctx, store.UpdateUserProfileInput{
			UserID:      user.ID,
			DisplayName: spec.Name,
			Handle:      spec.Handle,
		})
		if err != nil {
			return Manifest{}, fmt.Errorf("normalize user %s: %w", spec.Subject, err)
		}
		users = append(users, user)
		userManifest = append(userManifest, UserManifest{ID: user.ID, Name: spec.Name, Handle: spec.Handle, Email: spec.Email})
	}

	workspace, err := ensureWorkspace(ctx, st, users[0])
	if err != nil {
		return Manifest{}, err
	}
	for _, user := range users[1:] {
		if err := st.AddWorkspaceMember(ctx, workspace.ID, user.ID, store.WorkspaceRoleMember); err != nil {
			return Manifest{}, fmt.Errorf("add workspace member %s: %w", user.ID, err)
		}
	}

	channels, err := ensureChannels(ctx, st, workspace, users[0])
	if err != nil {
		return Manifest{}, err
	}
	messageIDs, err := ensureMessages(ctx, st, users, channels)
	if err != nil {
		return Manifest{}, err
	}

	channelManifest := make([]ChannelManifest, 0, len(seedChannels))
	for _, name := range seedChannels {
		channelManifest = append(channelManifest, ChannelManifest{ID: channels[name].ID, Name: name})
	}
	return Manifest{
		Version:    SeedVersion,
		Workspace:  workspace,
		Users:      userManifest,
		Channels:   channelManifest,
		MessageIDs: messageIDs,
	}, nil
}

func ensureWorkspace(ctx context.Context, st store.Store, owner store.User) (store.Workspace, error) {
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		return store.Workspace{}, fmt.Errorf("list workspaces: %w", err)
	}
	for _, workspace := range workspaces {
		if workspace.Slug == "fakeco" {
			return workspace, nil
		}
	}
	workspace, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "FakeCo", Slug: "fakeco"}, owner.ID)
	if err != nil {
		return store.Workspace{}, fmt.Errorf("create FakeCo workspace: %w", err)
	}
	return workspace, nil
}

func ensureChannels(ctx context.Context, st store.Store, workspace store.Workspace, owner store.User) (map[string]store.Channel, error) {
	items, err := st.ListChannels(ctx, workspace.ID, owner.ID)
	if err != nil {
		return nil, fmt.Errorf("list FakeCo channels: %w", err)
	}
	channels := make(map[string]store.Channel, len(seedChannels))
	for _, channel := range items {
		channels[channel.Name] = channel
	}
	for _, name := range seedChannels {
		if _, ok := channels[name]; ok {
			continue
		}
		channel, _, err := st.CreateChannel(ctx, store.CreateChannelInput{
			WorkspaceID: workspace.ID,
			Name:        name,
			Kind:        "public",
			UserID:      owner.ID,
		})
		if err != nil {
			return nil, fmt.Errorf("create channel %s: %w", name, err)
		}
		channels[name] = channel
	}
	return channels, nil
}

func ensureMessages(ctx context.Context, st store.Store, users []store.User, channels map[string]store.Channel) (map[string]string, error) {
	messageIDs := make(map[string]string)
	type messageSpec struct {
		Key     string
		Channel string
		Author  int
		Body    string
		Reply   *replySpec
	}
	messages := []messageSpec{
		{
			Key: "general-welcome", Channel: "general", Author: 0,
			Body:  "Welcome to FakeCo. This workspace contains synthetic test data only.",
			Reply: &replySpec{Key: "general-welcome-reply", Author: 1, Body: "I’ll use #e2e-canary for isolated OpenClaw round trips."},
		},
		{
			Key: "engineering-rollout", Channel: "engineering", Author: 1,
			Body:  "Gateway rollout checklist: ClickClack, OpenClaw, and ClawRouter.",
			Reply: &replySpec{Key: "engineering-rollout-reply", Author: 2, Body: "Telemetry stays metadata-only; prompts and replies stay out of metrics."},
		},
		{
			Key: "incidents-fc-1001", Channel: "incidents", Author: 2,
			Body:  "Synthetic incident FC-1001: canary latency elevated.",
			Reply: &replySpec{Key: "incidents-fc-1001-reply", Author: 0, Body: "Acknowledged. Keep this thread for deterministic E2E exercises."},
		},
		{
			Key: "canary-purpose", Channel: "e2e-canary", Author: 0,
			Body: "Automated canaries post here. Correlation markers are synthetic and safe to remove.",
		},
	}
	for _, spec := range messages {
		root, _, err := st.CreateMessage(ctx, store.CreateMessageInput{
			ChannelID: channels[spec.Channel].ID,
			AuthorID:  users[spec.Author].ID,
			Body:      spec.Body,
			Nonce:     "fakeco-seed-v1." + spec.Key,
		})
		if err != nil {
			return nil, fmt.Errorf("seed message %s: %w", spec.Key, err)
		}
		messageIDs[spec.Key] = root.ID
		if spec.Reply == nil {
			continue
		}
		reply, _, _, err := st.CreateThreadReply(ctx, store.CreateThreadReplyInput{
			RootMessageID: root.ID,
			AuthorID:      users[spec.Reply.Author].ID,
			Body:          spec.Reply.Body,
			Nonce:         "fakeco-seed-v1." + spec.Reply.Key,
		})
		if err != nil {
			return nil, fmt.Errorf("seed reply %s: %w", spec.Reply.Key, err)
		}
		messageIDs[spec.Reply.Key] = reply.ID
	}
	return messageIDs, nil
}

type replySpec struct {
	Key    string
	Author int
	Body   string
}
