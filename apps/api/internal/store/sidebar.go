package store

import "errors"

// MaxSidebarChannelOrderIDs bounds one workspace's saved channel order. A
// client with more channels than this saves the leading window and lets the
// server's default ordering carry the rest.
const MaxSidebarChannelOrderIDs = 500

// MaxSidebarChannelOrderWorkspaces bounds how many workspaces one patch may
// rewrite, so a single request cannot fan out into unbounded membership work.
const MaxSidebarChannelOrderWorkspaces = 100

// maxSidebarChannelIDLength matches the client's own id guard. Longer values
// cannot name a channel, so they are dropped with the other unknown ids.
const maxSidebarChannelIDLength = 128

func SidebarPreferencesPatchEmpty(patch SidebarPreferencesPatch) bool {
	return patch.ChannelOrder == nil
}

// NormalizeSidebarPreferencesPatch collapses repeated ids to their first
// position and drops ids that cannot name a channel. Ids that are well formed
// but unknown survive here and are filtered against the workspace's channels in
// the store, where the workspace is known.
func NormalizeSidebarPreferencesPatch(input SidebarPreferencesPatch) (SidebarPreferencesPatch, error) {
	if input.ChannelOrder == nil {
		return SidebarPreferencesPatch{}, nil
	}
	if len(input.ChannelOrder) > MaxSidebarChannelOrderWorkspaces {
		return SidebarPreferencesPatch{}, errors.New("channel_order lists too many workspaces")
	}
	normalized := make(map[string][]string, len(input.ChannelOrder))
	for workspaceID, ids := range input.ChannelOrder {
		if workspaceID == "" {
			return SidebarPreferencesPatch{}, errors.New("channel_order workspace id is required")
		}
		seen := make(map[string]struct{}, len(ids))
		order := make([]string, 0, len(ids))
		for _, id := range ids {
			if id == "" || len(id) > maxSidebarChannelIDLength {
				continue
			}
			if _, duplicate := seen[id]; duplicate {
				continue
			}
			seen[id] = struct{}{}
			order = append(order, id)
		}
		if len(order) > MaxSidebarChannelOrderIDs {
			return SidebarPreferencesPatch{}, errors.New("channel_order lists too many channels for one workspace")
		}
		normalized[workspaceID] = order
	}
	return SidebarPreferencesPatch{ChannelOrder: normalized}, nil
}

// FilterSidebarChannelOrder keeps only ids that are channels of the workspace,
// in the caller's order. A channel that was deleted since the client last read
// the sidebar is dropped instead of failing the save.
func FilterSidebarChannelOrder(order []string, workspaceChannelIDs []string) []string {
	known := make(map[string]struct{}, len(workspaceChannelIDs))
	for _, id := range workspaceChannelIDs {
		known[id] = struct{}{}
	}
	filtered := make([]string, 0, len(order))
	for _, id := range order {
		if _, ok := known[id]; !ok {
			continue
		}
		filtered = append(filtered, id)
	}
	return filtered
}
