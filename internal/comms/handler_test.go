package comms

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alekspetrov/pilot/internal/executor"
)

// mockMessenger implements Messenger for testing.
type mockMessenger struct {
	mu       sync.Mutex
	messages []sentMessage
}

type sentMessage struct {
	contextID string
	threadID  string
	text      string
}

func (m *mockMessenger) SendText(_ context.Context, contextID, threadID, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, sentMessage{contextID, threadID, text})
	return nil
}

func (m *mockMessenger) SendConfirmation(_ context.Context, contextID, threadID, taskID, description, projectPath string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, sentMessage{contextID, threadID, fmt.Sprintf("CONFIRM:%s:%s", taskID, description)})
	return "msg-ref-123", nil
}

func (m *mockMessenger) UpdateMessage(_ context.Context, contextID, msgRef, text string) error {
	return nil
}

func (m *mockMessenger) FormatGreeting(username string) string {
	if username != "" {
		return fmt.Sprintf("Hi %s!", username)
	}
	return "Hi!"
}

func (m *mockMessenger) FormatQuestionAck() string {
	return "Looking into that..."
}

func (m *mockMessenger) CleanOutput(output string) string {
	return strings.TrimSpace(output)
}

func (m *mockMessenger) FormatTaskResult(result *executor.ExecutionResult) string {
	if result.Success {
		return fmt.Sprintf("Task completed: %s", result.TaskID)
	}
	return fmt.Sprintf("Task failed: %s", result.TaskID)
}

func (m *mockMessenger) FormatProgressUpdate(taskID, phase string, progress int, message string) string {
	return fmt.Sprintf("%s: %s %d%% - %s", taskID, phase, progress, message)
}

func (m *mockMessenger) MaxMessageLen() int { return 4000 }

func (m *mockMessenger) ChunkContent(content string, maxLen int) []string {
	if len(content) <= maxLen {
		return []string{content}
	}
	var chunks []string
	for len(content) > 0 {
		end := maxLen
		if end > len(content) {
			end = len(content)
		}
		chunks = append(chunks, content[:end])
		content = content[end:]
	}
	return chunks
}

func (m *mockMessenger) lastMessage() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.messages) == 0 {
		return ""
	}
	return m.messages[len(m.messages)-1].text
}

func (m *mockMessenger) allMessages() []sentMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]sentMessage, len(m.messages))
	copy(result, m.messages)
	return result
}

// mockMemberResolver implements MemberResolver for testing.
type mockMemberResolver struct {
	mappings map[string]string
}

func (m *mockMemberResolver) ResolveMemberID(senderID string) (string, error) {
	if m.mappings == nil {
		return "", nil
	}
	return m.mappings[senderID], nil
}

func newTestHandler(messenger *mockMessenger) *Handler {
	return NewHandler(&HandlerConfig{
		Messenger:   messenger,
		ProjectPath: "/test/project",
	})
}

func TestHandler_HandleMessage_IntentDispatch(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		wantMsg string // substring expected in first sent message
	}{
		{
			name:    "greeting",
			text:    "hello",
			wantMsg: "Hi!",
		},
		{
			name:    "greeting with name",
			text:    "hey",
			wantMsg: "Hi!",
		},
		{
			name:    "confirmation yes",
			text:    "yes",
			wantMsg: "No pending task to confirm.",
		},
		{
			name:    "confirmation no",
			text:    "no",
			wantMsg: "No pending task to confirm.",
		},
		{
			name:    "rate limit message",
			text:    "",
			wantMsg: "", // empty text should produce no message
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &mockMessenger{}
			h := newTestHandler(m)

			h.HandleMessage(context.Background(), &IncomingMessage{
				ContextID: "chat-1",
				SenderID:  "user-1",
				Text:      tt.text,
			})

			if tt.wantMsg == "" {
				if len(m.allMessages()) != 0 {
					t.Errorf("expected no messages, got %d", len(m.allMessages()))
				}
				return
			}

			got := m.lastMessage()
			if !strings.Contains(got, tt.wantMsg) {
				t.Errorf("lastMessage() = %q, want substring %q", got, tt.wantMsg)
			}
		})
	}
}

func TestHandler_TrackSender(t *testing.T) {
	m := &mockMessenger{}
	h := newTestHandler(m)

	h.TrackSender("ctx-1", "sender-A")
	if got := h.GetLastSender("ctx-1"); got != "sender-A" {
		t.Errorf("GetLastSender() = %q, want %q", got, "sender-A")
	}

	// Overwrite
	h.TrackSender("ctx-1", "sender-B")
	if got := h.GetLastSender("ctx-1"); got != "sender-B" {
		t.Errorf("GetLastSender() = %q, want %q", got, "sender-B")
	}

	// Empty values should not track
	h.TrackSender("", "sender-C")
	h.TrackSender("ctx-2", "")

	if got := h.GetLastSender(""); got != "" {
		t.Error("empty context should not be tracked")
	}
	if got := h.GetLastSender("ctx-2"); got != "" {
		t.Error("empty sender should not be tracked")
	}
}

func TestHandler_HandleConfirmation_NoPending(t *testing.T) {
	m := &mockMessenger{}
	h := newTestHandler(m)

	h.HandleConfirmation(context.Background(), "ctx-1", "", true)

	got := m.lastMessage()
	if got != "No pending task to confirm." {
		t.Errorf("lastMessage() = %q, want 'No pending task to confirm.'", got)
	}
}

func TestHandler_HandleConfirmation_Cancel(t *testing.T) {
	m := &mockMessenger{}
	h := newTestHandler(m)

	h.SetPendingTask("ctx-1", &PendingTask{
		TaskID:      "TASK-1",
		Description: "test task",
		ContextID:   "ctx-1",
		CreatedAt:   time.Now(),
	})

	h.HandleConfirmation(context.Background(), "ctx-1", "", false)

	got := m.lastMessage()
	if !strings.Contains(got, "cancelled") {
		t.Errorf("lastMessage() = %q, want substring 'cancelled'", got)
	}

	if h.GetPendingTask("ctx-1") != nil {
		t.Error("pending task should be removed after cancellation")
	}
}

func TestHandler_HandleCallback(t *testing.T) {
	tests := []struct {
		name     string
		actionID string
		wantMsg  string
	}{
		{
			name:     "execute callback without pending",
			actionID: "execute_task",
			wantMsg:  "No pending task to confirm.",
		},
		{
			name:     "cancel callback without pending",
			actionID: "cancel_task",
			wantMsg:  "No pending task to confirm.",
		},
		{
			name:     "execute shorthand",
			actionID: "execute",
			wantMsg:  "No pending task to confirm.",
		},
		{
			name:     "cancel shorthand",
			actionID: "cancel",
			wantMsg:  "No pending task to confirm.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &mockMessenger{}
			h := newTestHandler(m)

			h.HandleCallback(context.Background(), "ctx-1", "user-1", tt.actionID)

			got := m.lastMessage()
			if !strings.Contains(got, tt.wantMsg) {
				t.Errorf("lastMessage() = %q, want substring %q", got, tt.wantMsg)
			}
		})
	}
}

func TestHandler_GetActiveProjectPath(t *testing.T) {
	m := &mockMessenger{}
	h := newTestHandler(m)

	// Default path
	if got := h.GetActiveProjectPath("ctx-1"); got != "/test/project" {
		t.Errorf("GetActiveProjectPath() = %q, want /test/project", got)
	}
}

func TestHandler_SetActiveProject_NoProjects(t *testing.T) {
	m := &mockMessenger{}
	h := newTestHandler(m)

	_, err := h.SetActiveProject("ctx-1", "myproject")
	if err == nil {
		t.Error("expected error when no projects configured")
	}
}

func TestHandler_CleanupExpiredTasks(t *testing.T) {
	m := &mockMessenger{}
	h := newTestHandler(m)

	// Add an expired task
	h.SetPendingTask("ctx-1", &PendingTask{
		TaskID:    "TASK-OLD",
		ContextID: "ctx-1",
		CreatedAt: time.Now().Add(-10 * time.Minute), // 10 min ago
	})

	// Add a fresh task
	h.SetPendingTask("ctx-2", &PendingTask{
		TaskID:    "TASK-NEW",
		ContextID: "ctx-2",
		CreatedAt: time.Now(),
	})

	h.CleanupExpiredTasks(context.Background())

	if h.GetPendingTask("ctx-1") != nil {
		t.Error("expired task should be cleaned up")
	}
	if h.GetPendingTask("ctx-2") == nil {
		t.Error("fresh task should NOT be cleaned up")
	}

	// Verify expiry notification was sent
	msgs := m.allMessages()
	found := false
	for _, msg := range msgs {
		if strings.Contains(msg.text, "TASK-OLD") && strings.Contains(msg.text, "expired") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected expiry notification for TASK-OLD")
	}
}

func TestHandler_ResolveMemberID(t *testing.T) {
	m := &mockMessenger{}
	resolver := &mockMemberResolver{
		mappings: map[string]string{
			"user-A": "member-1",
		},
	}

	h := NewHandler(&HandlerConfig{
		Messenger:      m,
		ProjectPath:    "/test/project",
		MemberResolver: resolver,
	})

	// Track sender first
	h.TrackSender("ctx-1", "user-A")

	memberID := h.resolveMemberID("ctx-1")
	if memberID != "member-1" {
		t.Errorf("resolveMemberID() = %q, want 'member-1'", memberID)
	}
}

func TestHandler_ResolveMemberID_NoResolver(t *testing.T) {
	m := &mockMessenger{}
	h := newTestHandler(m) // No member resolver

	h.TrackSender("ctx-1", "user-A")

	memberID := h.resolveMemberID("ctx-1")
	if memberID != "" {
		t.Errorf("resolveMemberID() without resolver = %q, want empty", memberID)
	}
}

func TestHandler_HandleMessage_RateLimit(t *testing.T) {
	m := &mockMessenger{}
	h := NewHandler(&HandlerConfig{
		Messenger:   m,
		ProjectPath: "/test/project",
		RateLimit: &RateLimitConfig{
			Enabled:           true,
			MessagesPerMinute: 1,
			TasksPerHour:      1,
			BurstSize:         1,
		},
	})

	// First message should succeed (greeting)
	h.HandleMessage(context.Background(), &IncomingMessage{
		ContextID: "ctx-1",
		SenderID:  "user-1",
		Text:      "hello",
	})

	// Second message should be rate limited
	h.HandleMessage(context.Background(), &IncomingMessage{
		ContextID: "ctx-1",
		SenderID:  "user-1",
		Text:      "hello again",
	})

	got := m.lastMessage()
	if !strings.Contains(got, "Rate limit exceeded") {
		t.Errorf("lastMessage() = %q, want rate limit message", got)
	}
}

func TestTruncateText(t *testing.T) {
	tests := []struct {
		text   string
		maxLen int
		want   string
	}{
		{"short", 10, "short"},
		{"hello world", 8, "hello..."},
		{"ab", 2, "ab"},
		{"abcd", 3, "abc"},
		{"a", 1, "a"},
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			got := truncateText(tt.text, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateText(%q, %d) = %q, want %q", tt.text, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestExtractPlanSummary(t *testing.T) {
	plan := "Line 1\n\nLine 2\nLine 3\n\nLine 4"
	summary := extractPlanSummary(plan)

	if !strings.Contains(summary, "Line 1") {
		t.Error("summary should contain Line 1")
	}
	if !strings.Contains(summary, "Line 4") {
		t.Error("summary should contain Line 4")
	}
}

func TestExtractPlanSummary_LongPlan(t *testing.T) {
	var lines []string
	for i := 0; i < 30; i++ {
		lines = append(lines, fmt.Sprintf("Step %d of the plan", i+1))
	}
	plan := strings.Join(lines, "\n")

	summary := extractPlanSummary(plan)

	// Should be capped at 15 lines
	summaryLines := strings.Split(summary, "\n")
	if len(summaryLines) > 15 {
		t.Errorf("expected max 15 lines, got %d", len(summaryLines))
	}
}
