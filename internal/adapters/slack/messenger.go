package slack

import (
	"context"
	"fmt"
)

// SlackMessenger implements comms.Messenger for Slack communication.
type SlackMessenger struct {
	client *Client
}

// NewSlackMessenger creates a new SlackMessenger wrapping the provided Slack client.
func NewSlackMessenger(client *Client) *SlackMessenger {
	return &SlackMessenger{
		client: client,
	}
}

// SendText sends a plain text message to the given channel.
func (s *SlackMessenger) SendText(ctx context.Context, contextID, text string) error {
	msg := &Message{
		Channel: contextID,
		Text:    text,
	}

	_, err := s.client.PostMessage(ctx, msg)
	return err
}

// SendConfirmation sends a task confirmation prompt with approve/reject buttons.
// Returns the message timestamp (messageRef) for later updates.
func (s *SlackMessenger) SendConfirmation(ctx context.Context, contextID, threadID, taskID, desc, project string) (messageRef string, err error) {
	blocks := BuildConfirmationBlocks(taskID, desc)

	msg := &InteractiveMessage{
		Channel: contextID,
		Text:    fmt.Sprintf("Task: %s", taskID),
		Blocks:  blocks,
	}

	resp, err := s.client.PostInteractiveMessage(ctx, msg)
	if err != nil {
		return "", err
	}

	return resp.TS, nil
}

// SendProgress updates an existing message with progress information.
// If updating fails, posts a new message instead.
func (s *SlackMessenger) SendProgress(ctx context.Context, contextID, messageRef, taskID, phase string, progress int, detail string) (newRef string, err error) {
	blocks := BuildProgressBlocks(taskID, phase, progress, detail)

	// Try to update the existing message
	err = s.client.UpdateInteractiveMessage(ctx, contextID, messageRef, blocks, "")
	if err == nil {
		return messageRef, nil
	}

	// If update fails, post a new message
	msg := &InteractiveMessage{
		Channel: contextID,
		Blocks:  blocks,
	}

	resp, err := s.client.PostInteractiveMessage(ctx, msg)
	if err != nil {
		return "", err
	}

	return resp.TS, nil
}

// SendResult sends the final task result with status and optional PR link.
func (s *SlackMessenger) SendResult(ctx context.Context, contextID, threadID, taskID string, success bool, output, prURL string) error {
	blocks := BuildResultBlocks(taskID, success, output, prURL)

	msg := &InteractiveMessage{
		Channel: contextID,
		Blocks:  blocks,
	}

	_, err := s.client.PostInteractiveMessage(ctx, msg)
	return err
}

// SendChunked sends long content split into platform-appropriate chunks.
// Slack has a 3800 character limit for text content.
func (s *SlackMessenger) SendChunked(ctx context.Context, contextID, threadID, content, prefix string) error {
	chunks := ChunkContent(content, s.MaxMessageLength())

	for i, chunk := range chunks {
		var text string
		if i == 0 && prefix != "" {
			text = prefix + "\n\n" + chunk
		} else {
			text = chunk
		}

		msg := &Message{
			Channel:  contextID,
			Text:     text,
			ThreadTS: threadID,
		}

		_, err := s.client.PostMessage(ctx, msg)
		if err != nil {
			return err
		}
	}

	return nil
}

// AcknowledgeCallback responds to a button/callback interaction.
// For Slack, this is a no-op as the HTTP response itself acknowledges the callback.
func (s *SlackMessenger) AcknowledgeCallback(ctx context.Context, callbackID string) error {
	return nil
}

// MaxMessageLength returns Slack's maximum single-message text length.
// Slack's documented limit is 4000 chars, but we use 3800 to account for Block Kit markup.
func (s *SlackMessenger) MaxMessageLength() int {
	return 3800
}
