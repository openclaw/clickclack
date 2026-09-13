package store

import "errors"

// ErrLastChannel is returned when deleting the only channel left in a workspace.
var ErrLastChannel = errors.New("a workspace must keep at least one channel")

// ErrProvisionedChannel is returned when deleting a channel the server recreates.
var ErrProvisionedChannel = errors.New("guest workspace channels are recreated on sign-in and cannot be deleted")

const (
	ChannelDeletionBlockedLastChannel        = "last_channel"
	ChannelDeletionBlockedProvisionedChannel = "provisioned_channel"
)

// ChannelDeletionCounts describes the visible content a channel deletion removes.
// Files are the uploads attached only to this channel.
type ChannelDeletionCounts struct {
	Messages      int64 `json:"messages"`
	ThreadReplies int64 `json:"thread_replies"`
	Pins          int64 `json:"pins"`
	Topics        int64 `json:"topics"`
	Files         int64 `json:"files"`
	FileBytes     int64 `json:"file_bytes"`
}

type ChannelDeletionPreview struct {
	Channel Channel               `json:"channel"`
	Counts  ChannelDeletionCounts `json:"counts"`
	// Blocker names the rule that prevents deletion, when one applies.
	Blocker string `json:"blocker,omitempty"`
}

// ChannelDeletion is the committed result of deleting a channel.
type ChannelDeletion struct {
	Channel  Channel
	Counts   ChannelDeletionCounts
	Event    Event
	Cleanups []PendingUploadCleanup
}

// ChannelDeletionBlocker returns the rule that prevents deleting a channel, or nil.
func ChannelDeletionBlocker(workspaceSlug, channelName string, workspaceChannels int64) error {
	// The Guests workspace recreates both channels on the next guest sign-in.
	if workspaceSlug == "guests" && (channelName == GuestChannelName || channelName == "general") {
		return ErrProvisionedChannel
	}
	if workspaceChannels <= 1 {
		return ErrLastChannel
	}
	return nil
}

// ChannelDeletionBlockerCode maps a blocker error to its API code.
func ChannelDeletionBlockerCode(err error) string {
	switch {
	case errors.Is(err, ErrLastChannel):
		return ChannelDeletionBlockedLastChannel
	case errors.Is(err, ErrProvisionedChannel):
		return ChannelDeletionBlockedProvisionedChannel
	default:
		return ""
	}
}
