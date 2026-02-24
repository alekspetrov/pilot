package telegram

import (
	"strconv"

	"github.com/alekspetrov/pilot/internal/comms"
)

// telegramMemberBridge adapts the Telegram MemberResolver to comms.MemberResolver.
// It converts string sender IDs back to int64 for the Telegram-specific resolver.
type telegramMemberBridge struct {
	resolver MemberResolver
}

func newTelegramMemberBridge(resolver MemberResolver) comms.MemberResolver {
	if resolver == nil {
		return nil
	}
	return &telegramMemberBridge{resolver: resolver}
}

func (b *telegramMemberBridge) ResolveMemberID(senderID string) (string, error) {
	telegramID, err := strconv.ParseInt(senderID, 10, 64)
	if err != nil {
		return "", nil // Non-numeric sender ID, skip
	}
	return b.resolver.ResolveTelegramIdentity(telegramID, "")
}
