package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/sqlite/storedb"
)

const managedChannelReconcileAttempts = 5

func (s *Store) ReconcileManagedChannel(ctx context.Context, input store.ReconcileManagedChannelInput) (store.ReconcileManagedChannelResult, error) {
	provider, ref, err := store.NormalizeManagedChannelIdentity(input.ExternalProvider, input.ExternalRef)
	if err != nil {
		return store.ReconcileManagedChannelResult{}, err
	}
	input.ExternalProvider = provider
	input.ExternalRef = ref
	for attempt := 0; attempt < managedChannelReconcileAttempts; attempt++ {
		result, err := s.reconcileManagedChannelOnce(ctx, input)
		if err == nil {
			return result, nil
		}
		if !isManagedChannelReconcileConflict(err) {
			return store.ReconcileManagedChannelResult{}, err
		}
		time.Sleep(time.Duration(attempt+1) * 5 * time.Millisecond)
	}
	return store.ReconcileManagedChannelResult{}, errors.New("could not reconcile managed channel after concurrent updates")
}

func (s *Store) reconcileManagedChannelOnce(ctx context.Context, input store.ReconcileManagedChannelInput) (store.ReconcileManagedChannelResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.ReconcileManagedChannelResult{}, err
	}
	defer tx.Rollback()
	if err := requireNonGuestTx(ctx, tx, input.WorkspaceID, input.UserID); err != nil {
		return store.ReconcileManagedChannelResult{}, err
	}
	if err := requireNoModerationBlockTx(ctx, tx, input.WorkspaceID, input.UserID); err != nil {
		return store.ReconcileManagedChannelResult{}, err
	}

	qtx := s.q.WithTx(tx)
	row, err := qtx.GetManagedChannelByIdentity(ctx, storedb.GetManagedChannelByIdentityParams{
		WorkspaceID:      input.WorkspaceID,
		ExternalProvider: sqlText(input.ExternalProvider),
		ExternalRef:      sqlText(input.ExternalRef),
	})
	switch {
	case err == nil:
		return reconcileExistingManagedChannel(ctx, tx, qtx, storeChannelFromGetManagedChannelByIdentity(row), input)
	case !errors.Is(err, sql.ErrNoRows):
		return store.ReconcileManagedChannelResult{}, err
	}

	name := slug(input.Name)
	if name == "" {
		return store.ReconcileManagedChannelResult{}, errors.New("managed channel name is required")
	}
	if name == store.GuestChannelName {
		return store.ReconcileManagedChannelResult{}, errors.New("guest channel name is reserved")
	}
	kind := strings.TrimSpace(input.Kind)
	if kind == "" {
		kind = "public"
	}
	provider := input.ExternalProvider
	ref := input.ExternalRef
	channel := store.Channel{
		ID:               newID("chn"),
		WorkspaceID:      input.WorkspaceID,
		Name:             name,
		Kind:             kind,
		CreatedAt:        now(),
		ExternalManaged:  true,
		ExternalProvider: &provider,
		ExternalRef:      &ref,
		ExternalURL:      optionalTrimmedString(input.ExternalURL),
		SidebarSection:   optionalTrimmedString(input.SidebarSection),
	}
	if input.Archived {
		value := now()
		channel.ArchivedAt = &value
	}
	routeID, err := newRouteID('C')
	if err != nil {
		return store.ReconcileManagedChannelResult{}, err
	}
	channel.RouteID = routeID
	if err := qtx.InsertChannel(ctx, storedb.InsertChannelParams{
		ID:               channel.ID,
		RouteID:          sqlText(channel.RouteID),
		WorkspaceID:      channel.WorkspaceID,
		Name:             channel.Name,
		Kind:             channel.Kind,
		CreatedAt:        channel.CreatedAt,
		ExternalManaged:  databaseBool(true),
		ExternalProvider: sqlText(input.ExternalProvider),
		ExternalRef:      sqlText(input.ExternalRef),
		ExternalUrl:      nullFromPtr(channel.ExternalURL),
		SidebarSection:   nullFromPtr(channel.SidebarSection),
	}); err != nil {
		return store.ReconcileManagedChannelResult{}, err
	}
	if channel.ArchivedAt != nil {
		if err := qtx.UpdateChannel(ctx, storedb.UpdateChannelParams{
			Name:             channel.Name,
			Kind:             channel.Kind,
			ArchivedAt:       nullFromPtr(channel.ArchivedAt),
			ExternalManaged:  databaseBool(true),
			ExternalProvider: sqlText(input.ExternalProvider),
			ExternalRef:      sqlText(input.ExternalRef),
			ExternalUrl:      nullFromPtr(channel.ExternalURL),
			SidebarSection:   nullFromPtr(channel.SidebarSection),
			ID:               channel.ID,
		}); err != nil {
			return store.ReconcileManagedChannelResult{}, err
		}
	}
	event, err := insertEvent(ctx, tx, channel.WorkspaceID, channel.ID, "channel.created", nil, map[string]string{"channel_id": channel.ID})
	if err != nil {
		return store.ReconcileManagedChannelResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return store.ReconcileManagedChannelResult{}, err
	}
	return store.ReconcileManagedChannelResult{Channel: channel, Action: store.ManagedChannelActionCreated, Event: &event}, nil
}

func reconcileExistingManagedChannel(ctx context.Context, tx *sql.Tx, qtx *storedb.Queries, channel store.Channel, input store.ReconcileManagedChannelInput) (store.ReconcileManagedChannelResult, error) {
	name := slug(input.Name)
	if name == "" {
		return store.ReconcileManagedChannelResult{}, errors.New("managed channel name is required")
	}
	if name == store.GuestChannelName {
		return store.ReconcileManagedChannelResult{}, errors.New("guest channel name is reserved")
	}
	kind := strings.TrimSpace(input.Kind)
	if kind == "" {
		kind = "public"
	}
	archivedAt := channel.ArchivedAt
	if input.Archived && archivedAt == nil {
		value := now()
		archivedAt = &value
	}
	if !input.Archived {
		archivedAt = nil
	}
	externalURL := optionalTrimmedString(input.ExternalURL)
	sidebarSection := optionalTrimmedString(input.SidebarSection)
	changed := !channel.ExternalManaged ||
		channel.Name != name ||
		channel.Kind != kind ||
		(channel.ArchivedAt == nil) != (archivedAt == nil) ||
		!equalOptionalString(channel.ExternalURL, externalURL) ||
		!equalOptionalString(channel.SidebarSection, sidebarSection)
	if !changed {
		if err := tx.Commit(); err != nil {
			return store.ReconcileManagedChannelResult{}, err
		}
		return store.ReconcileManagedChannelResult{Channel: channel, Action: store.ManagedChannelActionUnchanged}, nil
	}
	if err := qtx.UpdateChannel(ctx, storedb.UpdateChannelParams{
		Name:             name,
		Kind:             kind,
		ArchivedAt:       nullFromPtr(archivedAt),
		ExternalManaged:  databaseBool(true),
		ExternalProvider: nullFromPtr(channel.ExternalProvider),
		ExternalRef:      nullFromPtr(channel.ExternalRef),
		ExternalUrl:      nullFromPtr(externalURL),
		SidebarSection:   nullFromPtr(sidebarSection),
		ID:               channel.ID,
	}); err != nil {
		return store.ReconcileManagedChannelResult{}, err
	}
	event, err := insertEvent(ctx, tx, channel.WorkspaceID, channel.ID, "channel.updated", nil, map[string]any{"channel_id": channel.ID, "archived": archivedAt != nil})
	if err != nil {
		return store.ReconcileManagedChannelResult{}, err
	}
	channel.Name = name
	channel.Kind = kind
	channel.ArchivedAt = archivedAt
	channel.ExternalManaged = true
	channel.ExternalURL = externalURL
	channel.SidebarSection = sidebarSection
	if err := tx.Commit(); err != nil {
		return store.ReconcileManagedChannelResult{}, err
	}
	return store.ReconcileManagedChannelResult{Channel: channel, Action: store.ManagedChannelActionUpdated, Event: &event}, nil
}

func isManagedChannelReconcileConflict(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "idx_channels_managed_identity") ||
		strings.Contains(message, "channels.workspace_id, channels.external_provider, channels.external_ref") ||
		strings.Contains(message, "channels.workspace_id, channels.name") ||
		strings.Contains(message, "route_id") ||
		strings.Contains(message, "database is locked")
}
