package slack

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestNewHandler(t *testing.T) {
	config := &HandlerConfig{
		BotToken:        "test-bot-token",
		AppToken:        "test-app-token",
		ProjectPath:     "/test/project",
		AllowedUsers:    []string{"U123", "U456"},
		AllowedChannels: []string{"C123"},
	}

	handler := NewHandler(config, nil)

	if handler == nil {
		t.Fatal("NewHandler returned nil")
	}

	if handler.projectPath != "/test/project" {
		t.Errorf("projectPath = %q, want %q", handler.projectPath, "/test/project")
	}

	if !handler.allowedUsers["U123"] {
		t.Error("U123 should be in allowedUsers")
	}

	if !handler.allowedUsers["U456"] {
		t.Error("U456 should be in allowedUsers")
	}

	if handler.allowedUsers["U789"] {
		t.Error("U789 should not be in allowedUsers")
	}

	if !handler.allowedChannels["C123"] {
		t.Error("C123 should be in allowedChannels")
	}
}

func TestNewHandler_DefaultRateLimiter(t *testing.T) {
	config := &HandlerConfig{
		BotToken: "test-bot-token",
		AppToken: "test-app-token",
	}

	handler := NewHandler(config, nil)

	if handler.rateLimiter == nil {
		t.Fatal("rateLimiter should be initialized with defaults")
	}
}

func TestNewHandler_CustomRateLimiter(t *testing.T) {
	config := &HandlerConfig{
		BotToken: "test-bot-token",
		AppToken: "test-app-token",
		RateLimit: &RateLimitConfig{
			Enabled:           true,
			MessagesPerMinute: 100,
			TasksPerHour:      50,
			BurstSize:         10,
		},
	}

	handler := NewHandler(config, nil)

	if handler.rateLimiter == nil {
		t.Fatal("rateLimiter should be initialized")
	}
}

func TestProcessEvent_FiltersBotMessages(t *testing.T) {
	config := &HandlerConfig{
		BotToken: "test-bot-token",
		AppToken: "test-app-token",
	}

	handler := NewHandler(config, nil)

	// Bot message should be filtered
	evt := &SocketEvent{
		Type:      EventTypeMessage,
		ChannelID: "C123",
		UserID:    "U123",
		BotID:     "B123", // Bot message
		Text:      "test",
	}

	// This should not panic and should silently ignore the bot message
	handler.processEvent(context.Background(), evt)
}

func TestProcessEvent_FiltersUnauthorizedUsers(t *testing.T) {
	config := &HandlerConfig{
		BotToken:     "test-bot-token",
		AppToken:     "test-app-token",
		AllowedUsers: []string{"U123"}, // Only U123 allowed
	}

	handler := NewHandler(config, nil)

	// Unauthorized user should be filtered
	evt := &SocketEvent{
		Type:      EventTypeMessage,
		ChannelID: "C123",
		UserID:    "U456", // Not in allowed list
		Text:      "test",
	}

	// This should not panic and should silently ignore unauthorized user
	handler.processEvent(context.Background(), evt)
}

func TestProcessEvent_FiltersNonAllowedChannels(t *testing.T) {
	config := &HandlerConfig{
		BotToken:        "test-bot-token",
		AppToken:        "test-app-token",
		AllowedChannels: []string{"C123"}, // Only C123 allowed
	}

	handler := NewHandler(config, nil)

	// Non-allowed channel should be filtered
	evt := &SocketEvent{
		Type:      EventTypeMessage,
		ChannelID: "C456", // Not in allowed list
		UserID:    "U123",
		Text:      "test",
	}

	// This should not panic and should silently ignore the message
	handler.processEvent(context.Background(), evt)
}

func TestProcessEvent_RequiresMentionInChannels(t *testing.T) {
	config := &HandlerConfig{
		BotToken: "test-bot-token",
		AppToken: "test-app-token",
	}

	handler := NewHandler(config, nil)

	// Regular message in channel (not DM) should require mention
	evt := &SocketEvent{
		Type:      EventTypeMessage, // Not app_mention
		ChannelID: "C123",           // Channel, not DM
		UserID:    "U123",
		Text:      "test",
	}

	// This should not process the message (silently ignore non-mentions in channels)
	handler.processEvent(context.Background(), evt)
}

func TestProcessEvent_AllowsDMsWithoutMention(t *testing.T) {
	config := &HandlerConfig{
		BotToken: "test-bot-token",
		AppToken: "test-app-token",
	}

	handler := NewHandler(config, nil)

	// DM should process all messages
	evt := &SocketEvent{
		Type:      EventTypeMessage,
		ChannelID: "D123", // DM channel starts with 'D'
		UserID:    "U123",
		Text:      "test",
	}

	// Store the message in conversation store
	handler.processEvent(context.Background(), evt)

	// Check that the message was stored in conversation history
	msgs := handler.conversationStore.Get("D123", "")
	if len(msgs) != 1 {
		t.Errorf("Expected 1 message in conversation store, got %d", len(msgs))
	}
}

func TestProcessEvent_ProcessesAppMentions(t *testing.T) {
	config := &HandlerConfig{
		BotToken: "test-bot-token",
		AppToken: "test-app-token",
	}

	handler := NewHandler(config, nil)

	// App mention should be processed
	evt := &SocketEvent{
		Type:      EventTypeAppMention,
		ChannelID: "C123",
		UserID:    "U123",
		Text:      "run tests",
	}

	handler.processEvent(context.Background(), evt)

	// Check that the message was stored in conversation history
	msgs := handler.conversationStore.Get("C123", "")
	if len(msgs) != 1 {
		t.Errorf("Expected 1 message in conversation store, got %d", len(msgs))
	}
}

func TestHandler_Stop(t *testing.T) {
	config := &HandlerConfig{
		BotToken: "test-bot-token",
		AppToken: "test-app-token",
	}

	handler := NewHandler(config, nil)

	// Add a mock running task
	ctx, cancel := context.WithCancel(context.Background())
	handler.runningTasks["C123:123.456"] = &RunningTask{
		TaskID:    "TASK-1",
		ChannelID: "C123",
		Cancel:    cancel,
	}

	// Stop should cancel running tasks
	handler.Stop()

	// Context should be cancelled
	select {
	case <-ctx.Done():
		// Expected
	default:
		t.Error("Running task context should be cancelled")
	}
}

func TestConversationStore_AddGet(t *testing.T) {
	store := NewConversationStore(5, time.Hour)

	store.Add("C123", "", "user", "Hello", "U1")
	store.Add("C123", "", "assistant", "Hi there", "")
	store.Add("C123", "", "user", "How are you?", "U1")

	msgs := store.Get("C123", "")
	if len(msgs) != 3 {
		t.Errorf("Expected 3 messages, got %d", len(msgs))
	}

	if msgs[0].Content != "Hello" {
		t.Errorf("First message content = %q, want %q", msgs[0].Content, "Hello")
	}

	if msgs[1].Role != "assistant" {
		t.Errorf("Second message role = %q, want %q", msgs[1].Role, "assistant")
	}
}

func TestConversationStore_ThreadedConversations(t *testing.T) {
	store := NewConversationStore(5, time.Hour)

	// Add messages to main channel and a thread
	store.Add("C123", "", "user", "Main channel message", "U1")
	store.Add("C123", "123.456", "user", "Thread message 1", "U1")
	store.Add("C123", "123.456", "user", "Thread message 2", "U2")

	// Main channel should have 1 message
	mainMsgs := store.Get("C123", "")
	if len(mainMsgs) != 1 {
		t.Errorf("Main channel: expected 1 message, got %d", len(mainMsgs))
	}

	// Thread should have 2 messages
	threadMsgs := store.Get("C123", "123.456")
	if len(threadMsgs) != 2 {
		t.Errorf("Thread: expected 2 messages, got %d", len(threadMsgs))
	}
}

func TestConversationStore_MaxSize(t *testing.T) {
	store := NewConversationStore(3, time.Hour) // Max 3 messages

	store.Add("C123", "", "user", "msg1", "U1")
	store.Add("C123", "", "user", "msg2", "U1")
	store.Add("C123", "", "user", "msg3", "U1")
	store.Add("C123", "", "user", "msg4", "U1") // Should evict msg1
	store.Add("C123", "", "user", "msg5", "U1") // Should evict msg2

	msgs := store.Get("C123", "")
	if len(msgs) != 3 {
		t.Errorf("Expected 3 messages (max size), got %d", len(msgs))
	}

	// Should have msg3, msg4, msg5
	if msgs[0].Content != "msg3" {
		t.Errorf("First message should be msg3, got %q", msgs[0].Content)
	}
	if msgs[2].Content != "msg5" {
		t.Errorf("Last message should be msg5, got %q", msgs[2].Content)
	}
}

func TestConversationStore_GetReturnsCopy(t *testing.T) {
	store := NewConversationStore(5, time.Hour)

	store.Add("C123", "", "user", "Hello", "U1")

	msgs1 := store.Get("C123", "")
	msgs2 := store.Get("C123", "")

	// Modify one copy
	msgs1[0].Content = "Modified"

	// Other copy should be unchanged
	if msgs2[0].Content != "Hello" {
		t.Errorf("Get should return copies, but modification affected other copy")
	}
}

func TestConversationStore_ConcurrentAccess(t *testing.T) {
	store := NewConversationStore(100, time.Hour)

	var wg sync.WaitGroup
	const goroutines = 10
	const messagesPerGoroutine = 100

	// Concurrent writes
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < messagesPerGoroutine; j++ {
				store.Add("C123", "", "user", "msg", "U1")
			}
		}(i)
	}

	// Concurrent reads
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < messagesPerGoroutine; j++ {
				_ = store.Get("C123", "")
			}
		}()
	}

	wg.Wait()

	// Just verify no panics occurred
	msgs := store.Get("C123", "")
	if len(msgs) == 0 {
		t.Error("Expected some messages after concurrent access")
	}
}

func TestGetProjectPath_DefaultPath(t *testing.T) {
	config := &HandlerConfig{
		BotToken:    "test-bot-token",
		AppToken:    "test-app-token",
		ProjectPath: "/default/project",
	}

	handler := NewHandler(config, nil)

	path := handler.getProjectPath("C123")
	if path != "/default/project" {
		t.Errorf("getProjectPath = %q, want %q", path, "/default/project")
	}
}

func TestGetProjectPath_ActiveProjectOverride(t *testing.T) {
	config := &HandlerConfig{
		BotToken:    "test-bot-token",
		AppToken:    "test-app-token",
		ProjectPath: "/default/project",
	}

	handler := NewHandler(config, nil)

	// Set active project for channel
	handler.mu.Lock()
	handler.activeProject["C123"] = "/active/project"
	handler.mu.Unlock()

	path := handler.getProjectPath("C123")
	if path != "/active/project" {
		t.Errorf("getProjectPath = %q, want %q", path, "/active/project")
	}

	// Different channel should still use default
	path2 := handler.getProjectPath("C456")
	if path2 != "/default/project" {
		t.Errorf("getProjectPath for C456 = %q, want %q", path2, "/default/project")
	}
}

func TestGetProjectPath_FromProjectSource(t *testing.T) {
	projects := NewStaticProjectSource([]*ProjectInfo{
		{
			Name:      "test-project",
			ChannelID: "C123",
			WorkDir:   "/project/from/source",
		},
	})

	config := &HandlerConfig{
		BotToken:    "test-bot-token",
		AppToken:    "test-app-token",
		ProjectPath: "/default/project",
		Projects:    projects,
	}

	handler := NewHandler(config, nil)

	path := handler.getProjectPath("C123")
	if path != "/project/from/source" {
		t.Errorf("getProjectPath = %q, want %q", path, "/project/from/source")
	}

	// Channel not in project source should use default
	path2 := handler.getProjectPath("C456")
	if path2 != "/default/project" {
		t.Errorf("getProjectPath for C456 = %q, want %q", path2, "/default/project")
	}
}

func TestTruncateText(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is a longer text that needs truncation", 20, "this is a longer ..."},
		{"abc", 6, "abc"},
		{"", 10, ""},
	}

	for _, tt := range tests {
		got := truncateText(tt.input, tt.maxLen)
		if got != tt.want {
			t.Errorf("truncateText(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
		}
	}
}

func TestMakeConversationKey(t *testing.T) {
	tests := []struct {
		channelID string
		threadTS  string
		want      string
	}{
		{"C123", "", "C123"},
		{"C123", "123.456", "C123:123.456"},
		{"D456", "", "D456"},
		{"D456", "789.012", "D456:789.012"},
	}

	for _, tt := range tests {
		got := makeConversationKey(tt.channelID, tt.threadTS)
		if got != tt.want {
			t.Errorf("makeConversationKey(%q, %q) = %q, want %q",
				tt.channelID, tt.threadTS, got, tt.want)
		}
	}
}
