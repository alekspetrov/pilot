package slack

import (
	"context"

	"github.com/alekspetrov/pilot/internal/comms"
	"github.com/alekspetrov/pilot/internal/executor"
)

// slackMessenger adapts the Slack Client to the comms.Messenger interface.
type slackMessenger struct {
	apiClient *Client
}

// newSlackMessenger creates a Messenger backed by a Slack API client.
func newSlackMessenger(apiClient *Client) comms.Messenger {
	return &slackMessenger{apiClient: apiClient}
}

func (m *slackMessenger) SendText(ctx context.Context, contextID, threadID, text string) error {
	_, err := m.apiClient.PostMessage(ctx, &Message{
		Channel:  contextID,
		Text:     text,
		ThreadTS: threadID,
	})
	return err
}

func (m *slackMessenger) SendConfirmation(ctx context.Context, contextID, threadID, taskID, description, projectPath string) (string, error) {
	confirmMsg := FormatTaskConfirmation(taskID, description, projectPath)
	blocks := BuildConfirmationBlocks(taskID, truncateText(description, 500))

	resp, err := m.apiClient.PostInteractiveMessage(ctx, &InteractiveMessage{
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

func (m *slackMessenger) UpdateMessage(ctx context.Context, contextID, msgRef, text string) error {
	return m.apiClient.UpdateMessage(ctx, contextID, msgRef, &Message{
		Channel: contextID,
		Text:    text,
	})
}

func (m *slackMessenger) FormatGreeting(username string) string {
	return FormatGreeting(username)
}

func (m *slackMessenger) FormatQuestionAck() string {
	return FormatQuestionAck()
}

func (m *slackMessenger) CleanOutput(output string) string {
	return CleanInternalSignals(output)
}

func (m *slackMessenger) FormatTaskResult(result *executor.ExecutionResult) string {
	output := CleanInternalSignals(result.Output)
	return FormatTaskResult(output, result.Success, result.PRUrl)
}

func (m *slackMessenger) FormatProgressUpdate(taskID, phase string, progress int, message string) string {
	return FormatProgressUpdate(taskID, phase, progress, message)
}

func (m *slackMessenger) MaxMessageLen() int {
	return 3800 // Slack limit is 4096, use 3800 for safety
}

func (m *slackMessenger) ChunkContent(content string, maxLen int) []string {
	return ChunkContent(content, maxLen)
}
