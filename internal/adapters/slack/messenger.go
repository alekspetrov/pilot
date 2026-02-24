package slack

import (
	"context"
	"fmt"
	"time"
)

// SlackMessenger implements comms.Messenger for the Slack platform.
type SlackMessenger struct {
	client *Client
}

// NewSlackMessenger creates a new Slack messenger.
func NewSlackMessenger(client *Client) *SlackMessenger {
	return &SlackMessenger{client: client}
}

func (m *SlackMessenger) SendText(ctx context.Context, contextID, text string) error {
	_, err := m.client.PostMessage(ctx, &Message{
		Channel: contextID,
		Text:    text,
	})
	return err
}

func (m *SlackMessenger) SendConfirmation(ctx context.Context, contextID, threadID, taskID, desc, project string) (string, error) {
	confirmMsg := FormatTaskConfirmation(taskID, desc, project)
	blocks := BuildConfirmationBlocks(taskID, truncateText(desc, 500))

	resp, err := m.client.PostInteractiveMessage(ctx, &InteractiveMessage{
		Channel: contextID,
		Text:    confirmMsg,
		Blocks:  blocks,
	})
	if err != nil {
		return "", err
	}
	if resp != nil {
		return resp.TS, nil
	}
	return "", nil
}

func (m *SlackMessenger) SendProgress(ctx context.Context, contextID, messageRef, taskID, phase string, progress int, detail string) (string, error) {
	updateText := FormatProgressUpdate(taskID, phase, progress, detail)

	if messageRef != "" {
		// Update existing message
		err := m.client.UpdateMessage(ctx, contextID, messageRef, &Message{
			Channel: contextID,
			Text:    updateText,
		})
		if err == nil {
			return messageRef, nil
		}
	}

	// Send new message
	resp, err := m.client.PostMessage(ctx, &Message{
		Channel: contextID,
		Text:    updateText,
	})
	if err != nil {
		return "", err
	}
	if resp != nil {
		return resp.TS, nil
	}
	return "", nil
}

func (m *SlackMessenger) SendResult(ctx context.Context, contextID, threadID, taskID string, success bool, output, prURL string) error {
	resultMsg := FormatTaskResult(output, success, prURL)
	_, err := m.client.PostMessage(ctx, &Message{
		Channel:  contextID,
		Text:     resultMsg,
		ThreadTS: threadID,
	})
	return err
}

func (m *SlackMessenger) SendChunked(ctx context.Context, contextID, threadID, content, prefix string) error {
	if prefix != "" {
		content = fmt.Sprintf("%s\n\n%s", prefix, content)
	}

	chunks := ChunkContent(content, 3800)
	for i, chunk := range chunks {
		msg := chunk
		if len(chunks) > 1 {
			msg = fmt.Sprintf("📄 Part %d/%d\n\n%s", i+1, len(chunks), chunk)
		}

		_, err := m.client.PostMessage(ctx, &Message{
			Channel:  contextID,
			Text:     msg,
			ThreadTS: threadID,
		})
		if err != nil {
			return err
		}

		if i < len(chunks)-1 {
			time.Sleep(300 * time.Millisecond)
		}
	}
	return nil
}

func (m *SlackMessenger) AcknowledgeCallback(ctx context.Context, callbackID string) error {
	// Slack callbacks are acknowledged via the HTTP response, not a separate API call.
	// No-op here since Socket Mode handles this automatically.
	return nil
}

func (m *SlackMessenger) MaxMessageLength() int {
	return 4000
}
