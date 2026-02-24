package slack

import (
	"github.com/alekspetrov/pilot/internal/comms"
)

// slackMemberBridge adapts the Slack MemberResolver to comms.MemberResolver.
type slackMemberBridge struct {
	resolver MemberResolver
}

func newSlackMemberBridge(resolver MemberResolver) comms.MemberResolver {
	if resolver == nil {
		return nil
	}
	return &slackMemberBridge{resolver: resolver}
}

func (b *slackMemberBridge) ResolveMemberID(senderID string) (string, error) {
	return b.resolver.ResolveSlackIdentity(senderID, "")
}
