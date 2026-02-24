package telegram

import (
	"context"
	"fmt"
	"strconv"

	"github.com/alekspetrov/pilot/internal/executor"
)

// TelegramMessenger implements comms.Messenger interface for Telegram
type TelegramMessenger struct {
	client         *Client
	plainTextMode  bool
}

// NewTelegramMessenger creates a new Telegram messenger
func NewTelegramMessenger(client *Client, plainTextMode bool) *TelegramMessenger {
	return &TelegramMessenger{
		client:        client,
		plainTextMode: plainTextMode,
	}
}

// getParseMode returns the appropriate parse mode based on plainTextMode flag
func (tm *TelegramMessenger) getParseMode() string {
	if tm.plainTextMode {
		return ""
	}
	return "MarkdownV2"
}

// SendText sends a plain text message to a chat
func (tm *TelegramMessenger) SendText(chatID string, text string) (string, error) {
	ctx := context.Background()
	resp, err := tm.client.SendMessage(ctx, chatID, text, tm.getParseMode())
	if err != nil {
		return "", fmt.Errorf("failed to send text message: %w", err)
	}

	if resp == nil || resp.Result == nil {
		return "", fmt.Errorf("empty response from send message")
	}

	// Convert message ID to string reference
	messageRef := strconv.FormatInt(resp.Result.MessageID, 10)
	return messageRef, nil
}

// SendConfirmation sends a confirmation message with execute/cancel buttons
func (tm *TelegramMessenger) SendConfirmation(chatID string, taskID string, description string) (string, error) {
	ctx := context.Background()
	text := FormatTaskConfirmation(taskID, description, "")

	keyboard := [][]InlineKeyboardButton{
		{
			InlineKeyboardButton{
				Text:         "✅ Execute",
				CallbackData: "execute:" + taskID,
			},
			InlineKeyboardButton{
				Text:         "❌ Cancel",
				CallbackData: "cancel:" + taskID,
			},
		},
	}

	resp, err := tm.client.SendMessageWithKeyboard(ctx, chatID, text, tm.getParseMode(), keyboard)
	if err != nil {
		return "", fmt.Errorf("failed to send confirmation message: %w", err)
	}

	if resp == nil || resp.Result == nil {
		return "", fmt.Errorf("empty response from send message with keyboard")
	}

	// Convert message ID to string reference
	messageRef := strconv.FormatInt(resp.Result.MessageID, 10)
	return messageRef, nil
}

// SendProgress sends or updates a progress message
func (tm *TelegramMessenger) SendProgress(chatID string, taskID string, phase string, progress int, message string, messageRef *string) (string, error) {
	ctx := context.Background()
	text := FormatProgressUpdate(taskID, phase, progress, message)

	// If messageRef exists, try to edit the message
	if messageRef != nil && *messageRef != "" {
		messageID, err := strconv.ParseInt(*messageRef, 10, 64)
		if err != nil {
			// If we can't parse the message ID, send a new message instead
			resp, err := tm.client.SendMessage(ctx, chatID, text, tm.getParseMode())
			if err != nil {
				return "", fmt.Errorf("failed to send progress message: %w", err)
			}
			if resp == nil || resp.Result == nil {
				return "", fmt.Errorf("empty response from send message")
			}
			newRef := strconv.FormatInt(resp.Result.MessageID, 10)
			return newRef, nil
		}

		// Try to edit the existing message
		err = tm.client.EditMessage(ctx, chatID, messageID, text, tm.getParseMode())
		if err != nil {
			// If edit fails, send a new message instead
			resp, err := tm.client.SendMessage(ctx, chatID, text, tm.getParseMode())
			if err != nil {
				return "", fmt.Errorf("failed to send progress message after edit failure: %w", err)
			}
			if resp == nil || resp.Result == nil {
				return "", fmt.Errorf("empty response from send message")
			}
			newRef := strconv.FormatInt(resp.Result.MessageID, 10)
			return newRef, nil
		}

		return *messageRef, nil
	}

	// Send a new message if no reference provided
	resp, err := tm.client.SendMessage(ctx, chatID, text, tm.getParseMode())
	if err != nil {
		return "", fmt.Errorf("failed to send progress message: %w", err)
	}

	if resp == nil || resp.Result == nil {
		return "", fmt.Errorf("empty response from send message")
	}

	newRef := strconv.FormatInt(resp.Result.MessageID, 10)
	return newRef, nil
}

// SendResult sends a task result message
func (tm *TelegramMessenger) SendResult(chatID string, result *executor.ExecutionResult) (string, error) {
	ctx := context.Background()
	text := FormatTaskResult(result)

	resp, err := tm.client.SendMessage(ctx, chatID, text, tm.getParseMode())
	if err != nil {
		return "", fmt.Errorf("failed to send result message: %w", err)
	}

	if resp == nil || resp.Result == nil {
		return "", fmt.Errorf("empty response from send message")
	}

	// Convert message ID to string reference
	messageRef := strconv.FormatInt(resp.Result.MessageID, 10)
	return messageRef, nil
}

// SendChunked splits large messages and sends them sequentially
func (tm *TelegramMessenger) SendChunked(chatID string, text string) ([]string, error) {
	ctx := context.Background()
	maxLen := tm.MaxMessageLength()
	chunks := chunkContent(text, maxLen)

	var messageRefs []string
	for _, chunk := range chunks {
		resp, err := tm.client.SendMessage(ctx, chatID, chunk, tm.getParseMode())
		if err != nil {
			return messageRefs, fmt.Errorf("failed to send chunked message: %w", err)
		}

		if resp == nil || resp.Result == nil {
			return messageRefs, fmt.Errorf("empty response from send chunked message")
		}

		// Convert message ID to string reference
		messageRef := strconv.FormatInt(resp.Result.MessageID, 10)
		messageRefs = append(messageRefs, messageRef)
	}

	return messageRefs, nil
}

// AcknowledgeCallback acknowledges a callback query (e.g., button press)
func (tm *TelegramMessenger) AcknowledgeCallback(callbackID string, text string) error {
	ctx := context.Background()
	err := tm.client.AnswerCallback(ctx, callbackID, text)
	if err != nil {
		return fmt.Errorf("failed to acknowledge callback: %w", err)
	}
	return nil
}

// MaxMessageLength returns the maximum message length for Telegram
func (tm *TelegramMessenger) MaxMessageLength() int {
	return 4000
}
