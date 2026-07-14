package store

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
	"reaction.added",
	"reaction.removed",
	"thread.reply_created",
	"thread.state_updated",
	"workspace.ownership_transferred",
	"workspace.updated",
}

func IsDurableEventType(value string) bool {
	for _, eventType := range DurableEventTypes {
		if value == eventType {
			return true
		}
	}
	return false
}
