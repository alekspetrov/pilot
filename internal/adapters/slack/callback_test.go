package slack

import (
	"context"
	"testing"
	"time"
)

func TestHandleCallback_ExecuteTask(t *testing.T) {
	config := &HandlerConfig{
		BotToken:    "test-bot-token",
		AppToken:    "test-app-token",
		ProjectPath: "/test/project",
	}

	handler := NewHandler(config, nil)

	// Add a pending task
	taskID := "TASK-123"
	handler.mu.Lock()
	handler.pendingTasks["C123:"] = &PendingTask{
		TaskID:      taskID,
		Description: "Test task",
		ChannelID:   "C123",
		ThreadTS:    "",
		UserID:      "U123",
		CreatedAt:   time.Now(),
	}
	handler.mu.Unlock()

	// Create action for execute
	action := &InteractionAction{
		ActionID:  "execute_task",
		Value:     taskID,
		UserID:    "U123",
		ChannelID: "C123",
		MessageTS: "123.456",
	}

	// HandleCallback should return true for handled action
	// Note: actual execution will fail without a runner, but routing should work
	handled := handler.HandleCallback(context.Background(), action)
	if !handled {
		t.Error("HandleCallback should return true for execute_task action")
	}
}

func TestHandleCallback_CancelTask(t *testing.T) {
	config := &HandlerConfig{
		BotToken:    "test-bot-token",
		AppToken:    "test-app-token",
		ProjectPath: "/test/project",
	}

	handler := NewHandler(config, nil)

	// Add a pending task
	taskID := "TASK-456"
	handler.mu.Lock()
	handler.pendingTasks["C123:"] = &PendingTask{
		TaskID:      taskID,
		Description: "Test task to cancel",
		ChannelID:   "C123",
		ThreadTS:    "",
		UserID:      "U123",
		CreatedAt:   time.Now(),
	}
	handler.mu.Unlock()

	// Create action for cancel
	action := &InteractionAction{
		ActionID:  "cancel_task",
		Value:     taskID,
		UserID:    "U123",
		ChannelID: "C123",
		MessageTS: "123.456",
	}

	handled := handler.HandleCallback(context.Background(), action)
	if !handled {
		t.Error("HandleCallback should return true for cancel_task action")
	}

	// Verify task was removed from pending
	handler.mu.Lock()
	_, exists := handler.pendingTasks["C123:"]
	handler.mu.Unlock()

	if exists {
		t.Error("Pending task should be removed after cancel")
	}
}

func TestHandleCallback_ExecutePlan(t *testing.T) {
	config := &HandlerConfig{
		BotToken:    "test-bot-token",
		AppToken:    "test-app-token",
		ProjectPath: "/test/project",
	}

	handler := NewHandler(config, nil)

	// Add a pending plan
	taskID := "PLAN-789"
	handler.mu.Lock()
	handler.pendingTasks["C123:thread.123"] = &PendingTask{
		TaskID:      taskID,
		Description: "Test plan description",
		ChannelID:   "C123",
		ThreadTS:    "thread.123",
		UserID:      "U123",
		CreatedAt:   time.Now(),
	}
	handler.mu.Unlock()

	// Create action for execute plan
	action := &InteractionAction{
		ActionID:  "execute_plan",
		Value:     taskID,
		UserID:    "U123",
		ChannelID: "C123",
		MessageTS: "123.456",
	}

	handled := handler.HandleCallback(context.Background(), action)
	if !handled {
		t.Error("HandleCallback should return true for execute_plan action")
	}
}

func TestHandleCallback_CancelPlan(t *testing.T) {
	config := &HandlerConfig{
		BotToken:    "test-bot-token",
		AppToken:    "test-app-token",
		ProjectPath: "/test/project",
	}

	handler := NewHandler(config, nil)

	// Add a pending plan
	taskID := "PLAN-101"
	handler.mu.Lock()
	handler.pendingTasks["C123:thread.456"] = &PendingTask{
		TaskID:      taskID,
		Description: "Test plan to cancel",
		ChannelID:   "C123",
		ThreadTS:    "thread.456",
		UserID:      "U123",
		CreatedAt:   time.Now(),
	}
	handler.mu.Unlock()

	// Create action for cancel plan
	action := &InteractionAction{
		ActionID:  "cancel_plan",
		Value:     taskID,
		UserID:    "U123",
		ChannelID: "C123",
		MessageTS: "123.456",
	}

	handled := handler.HandleCallback(context.Background(), action)
	if !handled {
		t.Error("HandleCallback should return true for cancel_plan action")
	}

	// Verify plan was removed from pending
	handler.mu.Lock()
	_, exists := handler.pendingTasks["C123:thread.456"]
	handler.mu.Unlock()

	if exists {
		t.Error("Pending plan should be removed after cancel")
	}
}

func TestHandleCallback_UnknownAction(t *testing.T) {
	config := &HandlerConfig{
		BotToken:    "test-bot-token",
		AppToken:    "test-app-token",
		ProjectPath: "/test/project",
	}

	handler := NewHandler(config, nil)

	action := &InteractionAction{
		ActionID:  "unknown_action",
		Value:     "some-value",
		UserID:    "U123",
		ChannelID: "C123",
	}

	handled := handler.HandleCallback(context.Background(), action)
	if handled {
		t.Error("HandleCallback should return false for unknown action")
	}
}

func TestHandleCallback_NilAction(t *testing.T) {
	config := &HandlerConfig{
		BotToken:    "test-bot-token",
		AppToken:    "test-app-token",
		ProjectPath: "/test/project",
	}

	handler := NewHandler(config, nil)

	handled := handler.HandleCallback(context.Background(), nil)
	if handled {
		t.Error("HandleCallback should return false for nil action")
	}
}

func TestHandleCallback_TaskNotFound(t *testing.T) {
	config := &HandlerConfig{
		BotToken:    "test-bot-token",
		AppToken:    "test-app-token",
		ProjectPath: "/test/project",
	}

	handler := NewHandler(config, nil)

	// No pending tasks added

	action := &InteractionAction{
		ActionID:  "execute_task",
		Value:     "NONEXISTENT-TASK",
		UserID:    "U123",
		ChannelID: "C123",
		MessageTS: "123.456",
	}

	handled := handler.HandleCallback(context.Background(), action)
	if handled {
		t.Error("HandleCallback should return false when task not found")
	}
}

func TestHandleCallback_EmptyValue(t *testing.T) {
	config := &HandlerConfig{
		BotToken:    "test-bot-token",
		AppToken:    "test-app-token",
		ProjectPath: "/test/project",
	}

	handler := NewHandler(config, nil)

	action := &InteractionAction{
		ActionID:  "execute_task",
		Value:     "",
		UserID:    "U123",
		ChannelID: "C123",
	}

	handled := handler.HandleCallback(context.Background(), action)
	if handled {
		t.Error("HandleCallback should return false for empty value")
	}
}

func TestWireHandlerToInteractions(t *testing.T) {
	config := &HandlerConfig{
		BotToken:    "test-bot-token",
		AppToken:    "test-app-token",
		ProjectPath: "/test/project",
	}

	handler := NewHandler(config, nil)
	interactionHandler := NewInteractionHandler("test-signing-secret")

	// Wire them together
	WireHandlerToInteractions(handler, interactionHandler)

	// Verify onAction was set
	if interactionHandler.onAction == nil {
		t.Error("WireHandlerToInteractions should set onAction callback")
	}
}

func TestWireHandlerToInteractions_NilHandlers(t *testing.T) {
	// Should not panic with nil handlers
	WireHandlerToInteractions(nil, nil)
	WireHandlerToInteractions(&Handler{}, nil)
	WireHandlerToInteractions(nil, &InteractionHandler{})
}

func TestCallbackRouting(t *testing.T) {
	tests := []struct {
		name     string
		actionID string
		value    string
		hasPendingTask bool
		wantHandled bool
	}{
		{
			name:           "execute_task with pending task",
			actionID:       "execute_task",
			value:          "TASK-1",
			hasPendingTask: true,
			wantHandled:    true,
		},
		{
			name:           "execute_task without pending task",
			actionID:       "execute_task",
			value:          "TASK-1",
			hasPendingTask: false,
			wantHandled:    false,
		},
		{
			name:           "cancel_task with pending task",
			actionID:       "cancel_task",
			value:          "TASK-2",
			hasPendingTask: true,
			wantHandled:    true,
		},
		{
			name:           "cancel_task without pending task",
			actionID:       "cancel_task",
			value:          "TASK-2",
			hasPendingTask: false,
			wantHandled:    true, // Cancel still returns true even if no task found
		},
		{
			name:           "execute_plan with pending plan",
			actionID:       "execute_plan",
			value:          "PLAN-1",
			hasPendingTask: true,
			wantHandled:    true,
		},
		{
			name:           "cancel_plan with pending plan",
			actionID:       "cancel_plan",
			value:          "PLAN-2",
			hasPendingTask: true,
			wantHandled:    true,
		},
		{
			name:           "unknown action",
			actionID:       "something_else",
			value:          "VALUE",
			hasPendingTask: false,
			wantHandled:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &HandlerConfig{
				BotToken:    "test-bot-token",
				AppToken:    "test-app-token",
				ProjectPath: "/test/project",
			}

			handler := NewHandler(config, nil)

			if tt.hasPendingTask {
				handler.mu.Lock()
				handler.pendingTasks["C123:"] = &PendingTask{
					TaskID:      tt.value,
					Description: "Test description",
					ChannelID:   "C123",
					ThreadTS:    "",
					UserID:      "U123",
					CreatedAt:   time.Now(),
				}
				handler.mu.Unlock()
			}

			action := &InteractionAction{
				ActionID:  tt.actionID,
				Value:     tt.value,
				UserID:    "U123",
				ChannelID: "C123",
				MessageTS: "123.456",
			}

			got := handler.HandleCallback(context.Background(), action)
			if got != tt.wantHandled {
				t.Errorf("HandleCallback() = %v, want %v", got, tt.wantHandled)
			}
		})
	}
}
