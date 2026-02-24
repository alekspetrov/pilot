package telegram

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// TelegramMessenger implements comms.Messenger for the Telegram platform.
type TelegramMessenger struct {
	client        *Client
	plainTextMode bool
}

// NewTelegramMessenger creates a new Telegram messenger.
func NewTelegramMessenger(client *Client, plainTextMode bool) *TelegramMessenger {
	return &TelegramMessenger{
		client:        client,
		plainTextMode: plainTextMode,
	}
}

func (m *TelegramMessenger) SendText(ctx context.Context, contextID, text string) error {
	_, err := m.client.SendMessage(ctx, contextID, text, "")
	return err
}

func (m *TelegramMessenger) SendConfirmation(ctx context.Context, contextID, threadID, taskID, desc, project string) (string, error) {
	confirmMsg := FormatTaskConfirmation(taskID, desc, project)

	resp, err := m.client.SendMessageWithKeyboard(ctx, contextID, confirmMsg, "",
		[][]InlineKeyboardButton{
			{
				{Text: "✅ Execute", CallbackData: "execute"},
				{Text: "❌ Cancel", CallbackData: "cancel"},
			},
		})

	if err != nil {
		return "", err
	}

	if resp != nil && resp.Result != nil {
		return strconv.FormatInt(resp.Result.MessageID, 10), nil
	}
	return "", nil
}

func (m *TelegramMessenger) SendProgress(ctx context.Context, contextID, messageRef, taskID, phase string, progress int, detail string) (string, error) {
	updateText := FormatProgressUpdate(taskID, phase, progress, detail)

	if messageRef != "" {
		// Edit existing message
		msgID, err := strconv.ParseInt(messageRef, 10, 64)
		if err == nil {
			if editErr := m.client.EditMessage(ctx, contextID, msgID, updateText, ""); editErr == nil {
				return messageRef, nil
			}
		}
	}

	// Send new message
	resp, err := m.client.SendMessage(ctx, contextID, updateText, "")
	if err != nil {
		return "", err
	}
	if resp != nil && resp.Result != nil {
		return strconv.FormatInt(resp.Result.MessageID, 10), nil
	}
	return "", nil
}

func (m *TelegramMessenger) SendResult(ctx context.Context, contextID, threadID, taskID string, success bool, output, prURL string) error {
	var sb strings.Builder
	if success {
		sb.WriteString(fmt.Sprintf("✅ Task completed\n%s", taskID))
		if prURL != "" {
			sb.WriteString(fmt.Sprintf("\n\n🔗 PR: %s", prURL))
		}
		if output != "" {
			summary := truncateDescription(output, 1000)
			sb.WriteString(fmt.Sprintf("\n\n%s", summary))
		}
	} else {
		sb.WriteString(fmt.Sprintf("❌ Task failed\n%s", taskID))
		if output != "" {
			errMsg := output
			if len(errMsg) > 400 {
				errMsg = errMsg[:400] + "..."
			}
			sb.WriteString(fmt.Sprintf("\n\n%s", errMsg))
		}
	}

	_, err := m.client.SendMessage(ctx, contextID, sb.String(), "")
	return err
}

func (m *TelegramMessenger) SendChunked(ctx context.Context, contextID, threadID, content, prefix string) error {
	if prefix != "" {
		content = fmt.Sprintf("%s\n\n%s", prefix, content)
	}

	chunks := chunkContent(content, 3800)
	for i, chunk := range chunks {
		msg := chunk
		if len(chunks) > 1 {
			msg = fmt.Sprintf("📄 Part %d/%d\n\n%s", i+1, len(chunks), chunk)
		}
		_, err := m.client.SendMessage(ctx, contextID, msg, "")
		if err != nil {
			return err
		}
		if i < len(chunks)-1 {
			time.Sleep(500 * time.Millisecond)
		}
	}
	return nil
}

func (m *TelegramMessenger) AcknowledgeCallback(ctx context.Context, callbackID string) error {
	return m.client.AnswerCallback(ctx, callbackID, "")
}

func (m *TelegramMessenger) MaxMessageLength() int {
	return 4096
}
