package telegram

import (
	"context"
	"strconv"

	"github.com/alekspetrov/pilot/internal/comms"
	"github.com/alekspetrov/pilot/internal/executor"
)

// telegramMessenger adapts the Telegram Client to the comms.Messenger interface.
type telegramMessenger struct {
	client        *Client
	plainTextMode bool
}

// newTelegramMessenger creates a Messenger backed by a Telegram Client.
func newTelegramMessenger(client *Client, plainTextMode bool) comms.Messenger {
	return &telegramMessenger{client: client, plainTextMode: plainTextMode}
}

func (m *telegramMessenger) SendText(ctx context.Context, contextID, threadID, text string) error {
	parseMode := ""
	if !m.plainTextMode {
		parseMode = "Markdown"
	}
	_, err := m.client.SendMessage(ctx, contextID, text, parseMode)
	return err
}

func (m *telegramMessenger) SendConfirmation(ctx context.Context, contextID, threadID, taskID, description, projectPath string) (string, error) {
	confirmMsg := FormatTaskConfirmation(taskID, description, projectPath)

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

func (m *telegramMessenger) UpdateMessage(ctx context.Context, contextID, msgRef, text string) error {
	msgID, err := strconv.ParseInt(msgRef, 10, 64)
	if err != nil {
		return err
	}
	return m.client.EditMessage(ctx, contextID, msgID, text, "")
}

func (m *telegramMessenger) FormatGreeting(username string) string {
	return FormatGreeting(username)
}

func (m *telegramMessenger) FormatQuestionAck() string {
	return FormatQuestionAck()
}

func (m *telegramMessenger) CleanOutput(output string) string {
	return cleanInternalSignals(output)
}

func (m *telegramMessenger) FormatTaskResult(result *executor.ExecutionResult) string {
	return FormatTaskResult(result)
}

func (m *telegramMessenger) FormatProgressUpdate(taskID, phase string, progress int, message string) string {
	return FormatProgressUpdate(taskID, phase, progress, message)
}

func (m *telegramMessenger) MaxMessageLen() int {
	return 3800 // Telegram limit is 4096, use 3800 for safety
}

func (m *telegramMessenger) ChunkContent(content string, maxLen int) []string {
	return chunkContent(content, maxLen)
}
