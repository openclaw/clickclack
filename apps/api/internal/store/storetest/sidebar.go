package storetest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

// SidebarPreferences exercises the same persistence contract on both databases.
func SidebarPreferences(t *testing.T, st interface {
	store.Store
	ExportJSON(context.Context, io.Writer) error
}) {
	t.Helper()
	ctx := context.Background()
	suffix := fmt.Sprint(time.Now().UnixNano())
	user, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Sidebar", Email: suffix + "@sidebar.test"})
	if err != nil {
		t.Fatal(err)
	}
	workspaces := make([]store.Workspace, 2)
	channels := make([]string, 2)
	for i := range workspaces {
		workspaces[i], err = st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Sidebar", Slug: fmt.Sprintf("sidebar-%s-%d", suffix, i)}, user.ID)
		if err != nil {
			t.Fatal(err)
		}
		channel, _, err := st.CreateChannel(ctx, store.CreateChannelInput{WorkspaceID: workspaces[i].ID, UserID: user.ID, Name: "order", Kind: "public"})
		if err != nil {
			t.Fatal(err)
		}
		channels[i] = channel.ID
	}
	patch := func(orders map[string][]string) (store.CurrentUserState, error) {
		return st.UpdateCurrentUser(ctx, store.UpdateCurrentUserInput{UserID: user.ID, SidebarPreferences: &store.SidebarPreferencesPatch{ChannelOrder: orders}})
	}
	prefs, err := st.GetSidebarPreferences(ctx, user.ID)
	if err != nil || prefs != nil {
		t.Fatalf("initial preferences: %v %v", prefs, err)
	}
	for i, workspace := range workspaces {
		if _, err := patch(map[string][]string{workspace.ID: {channels[i], "missing", channels[1-i], channels[i]}}); err != nil {
			t.Fatal(err)
		}
	}
	want := map[string][]string{workspaces[0].ID: {channels[0]}, workspaces[1].ID: {channels[1]}}
	check := func() {
		t.Helper()
		prefs, err := st.GetSidebarPreferences(ctx, user.ID)
		if err != nil || prefs == nil || !reflect.DeepEqual(prefs.ChannelOrder, want) {
			t.Fatalf("preferences: %#v; error: %v; want: %v", prefs, err, want)
		}
	}
	check()
	dark := "dark"
	if _, err := st.UpdateCurrentUser(ctx, store.UpdateCurrentUserInput{UserID: user.ID, AppearancePreferences: &store.AppearancePreferencesPatch{ColorMode: &dark}}); err != nil {
		t.Fatal(err)
	}
	check()
	if _, err := patch(map[string][]string{workspaces[0].ID: {}}); err != nil {
		t.Fatal(err)
	}
	want[workspaces[0].ID] = []string{}
	check()
	appearance, err := st.GetAppearancePreferences(ctx, user.ID)
	if err != nil || appearance == nil || appearance.ColorMode != dark {
		t.Fatalf("appearance changed: %v %v", appearance, err)
	}
	// A denied workspace must roll back valid workspace edits in the same patch.
	if _, err := patch(map[string][]string{workspaces[1].ID: {}, "not-a-member": {}}); !errors.Is(err, store.ErrNotWorkspaceMember) {
		t.Fatalf("membership error: %v", err)
	}
	check()
	tooMany := make([]string, store.MaxSidebarChannelOrderIDs+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprint(i)
	}
	if _, err := patch(map[string][]string{workspaces[0].ID: tooMany}); err == nil {
		t.Fatal("accepted oversized order")
	}
	check()
	var output bytes.Buffer
	if err := st.ExportJSON(ctx, &output); err != nil {
		t.Fatal(err)
	}
	var snapshot struct {
		Orders []struct {
			UserID      string `json:"user_id"`
			WorkspaceID string `json:"workspace_id"`
			ChannelIDs  string `json:"channel_ids"`
		} `json:"user_sidebar_channel_order"`
	}
	if err := json.Unmarshal(output.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	exported := map[string][]string{}
	for _, row := range snapshot.Orders {
		if row.UserID != user.ID {
			continue
		}
		var order []string
		if err := json.Unmarshal([]byte(row.ChannelIDs), &order); err != nil {
			t.Fatal(err)
		}
		exported[row.WorkspaceID] = order
	}
	if !reflect.DeepEqual(exported, want) {
		t.Fatalf("exported sidebar preferences: %v; want: %v", exported, want)
	}
}
