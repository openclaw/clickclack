package store

import "slices"

// DurableEventTypes enumerates every event type persisted to the event log and
// eligible for outgoing event subscriptions.
var DurableEventTypes = []string{
	"channel.created",
	"channel.read",
	"channel.updated",
	"dm.read",
	"member.moderation_updated",
	"message.created",
	"message.deleted",
	"message.updated",
	"pin.added",
	"pin.removed",
	"reaction.added",
	"reaction.removed",
	"thread.reply_created",
	"thread.state_updated",
	"workspace.ownership_transferred",
	"workspace.updated",
}

func IsDurableEventType(value string) bool {
	return slices.Contains(DurableEventTypes, value)
}
