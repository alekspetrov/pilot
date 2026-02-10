package slack

import (
	"testing"
	"time"
)

func TestRateLimiter_AllowMessage(t *testing.T) {
	config := &RateLimitConfig{
		Enabled:           true,
		MessagesPerMinute: 10,
		TasksPerHour:      5,
		BurstSize:         3,
	}
	limiter := NewRateLimiter(config)

	channelID := "C123456789"

	// First 3 messages should be allowed (burst)
	for i := 0; i < 3; i++ {
		if !limiter.AllowMessage(channelID) {
			t.Errorf("Message %d should be allowed (burst)", i+1)
		}
	}

	// 4th message should be blocked (burst exhausted)
	if limiter.AllowMessage(channelID) {
		t.Error("Message 4 should be blocked (burst exhausted)")
	}
}

func TestRateLimiter_AllowTask(t *testing.T) {
	config := &RateLimitConfig{
		Enabled:           true,
		MessagesPerMinute: 20,
		TasksPerHour:      5,
		BurstSize:         2,
	}
	limiter := NewRateLimiter(config)

	channelID := "C987654321"

	// First 2 tasks should be allowed (burst)
	for i := 0; i < 2; i++ {
		if !limiter.AllowTask(channelID) {
			t.Errorf("Task %d should be allowed (burst)", i+1)
		}
	}

	// 3rd task should be blocked
	if limiter.AllowTask(channelID) {
		t.Error("Task 3 should be blocked (burst exhausted)")
	}
}

func TestRateLimiter_Disabled(t *testing.T) {
	config := &RateLimitConfig{
		Enabled:           false,
		MessagesPerMinute: 1,
		TasksPerHour:      1,
		BurstSize:         1,
	}
	limiter := NewRateLimiter(config)

	channelID := "C111222333"

	// All requests should be allowed when disabled
	for i := 0; i < 100; i++ {
		if !limiter.AllowMessage(channelID) {
			t.Error("Message should be allowed when rate limiting is disabled")
		}
		if !limiter.AllowTask(channelID) {
			t.Error("Task should be allowed when rate limiting is disabled")
		}
	}
}

func TestRateLimiter_TokenRefill(t *testing.T) {
	config := &RateLimitConfig{
		Enabled:           true,
		MessagesPerMinute: 60, // 1 per second
		TasksPerHour:      60,
		BurstSize:         1,
	}
	limiter := NewRateLimiter(config)

	channelID := "C444555666"

	// Use burst
	if !limiter.AllowMessage(channelID) {
		t.Error("First message should be allowed")
	}

	// Second message should be blocked
	if limiter.AllowMessage(channelID) {
		t.Error("Second message should be blocked (burst exhausted)")
	}

	// Wait for token to refill (1 second = 1 token at 60/min)
	time.Sleep(1100 * time.Millisecond)

	// Should have ~1 token now
	if !limiter.AllowMessage(channelID) {
		t.Error("Message should be allowed after refill")
	}
}

func TestRateLimiter_PerChannelIsolation(t *testing.T) {
	config := &RateLimitConfig{
		Enabled:           true,
		MessagesPerMinute: 10,
		TasksPerHour:      5,
		BurstSize:         2,
	}
	limiter := NewRateLimiter(config)

	channelID1 := "C111"
	channelID2 := "C222"

	// Use up channel1's burst
	limiter.AllowMessage(channelID1)
	limiter.AllowMessage(channelID1)

	// Channel1 should be blocked
	if limiter.AllowMessage(channelID1) {
		t.Error("Channel1 should be blocked")
	}

	// Channel2 should still have full burst
	if !limiter.AllowMessage(channelID2) {
		t.Error("Channel2 should have full burst")
	}
	if !limiter.AllowMessage(channelID2) {
		t.Error("Channel2 should have full burst")
	}
}

func TestRateLimiter_GetRemaining(t *testing.T) {
	config := &RateLimitConfig{
		Enabled:           true,
		MessagesPerMinute: 20,
		TasksPerHour:      10,
		BurstSize:         5,
	}
	limiter := NewRateLimiter(config)

	channelID := "C777888999"

	// Initial should be burst size
	if remaining := limiter.GetRemainingMessages(channelID); remaining != 5 {
		t.Errorf("Expected 5 remaining messages, got %d", remaining)
	}

	// Use one
	limiter.AllowMessage(channelID)

	if remaining := limiter.GetRemainingMessages(channelID); remaining != 4 {
		t.Errorf("Expected 4 remaining messages, got %d", remaining)
	}
}

func TestRateLimiter_GetRemainingTasks(t *testing.T) {
	config := &RateLimitConfig{
		Enabled:           true,
		MessagesPerMinute: 20,
		TasksPerHour:      10,
		BurstSize:         3,
	}
	limiter := NewRateLimiter(config)

	channelID := "C123123123"

	// Initial should be burst size
	if remaining := limiter.GetRemainingTasks(channelID); remaining != 3 {
		t.Errorf("Expected 3 remaining tasks, got %d", remaining)
	}

	// Use one
	limiter.AllowTask(channelID)

	if remaining := limiter.GetRemainingTasks(channelID); remaining != 2 {
		t.Errorf("Expected 2 remaining tasks, got %d", remaining)
	}
}

func TestRateLimiter_GetRemainingWhenDisabled(t *testing.T) {
	config := &RateLimitConfig{
		Enabled:           false,
		MessagesPerMinute: 10,
		TasksPerHour:      5,
		BurstSize:         2,
	}
	limiter := NewRateLimiter(config)

	channelID := "C000000000"

	// Should return -1 (unlimited) when disabled
	if remaining := limiter.GetRemainingMessages(channelID); remaining != -1 {
		t.Errorf("Expected -1 (unlimited) when disabled, got %d", remaining)
	}
	if remaining := limiter.GetRemainingTasks(channelID); remaining != -1 {
		t.Errorf("Expected -1 (unlimited) when disabled, got %d", remaining)
	}
}

func TestRateLimiter_Cleanup(t *testing.T) {
	config := DefaultRateLimitConfig()
	limiter := NewRateLimiter(config)

	// Create some buckets
	limiter.AllowMessage("C111")
	limiter.AllowMessage("C222")

	// Verify buckets exist
	limiter.mu.Lock()
	initialCount := len(limiter.buckets)
	limiter.mu.Unlock()

	if initialCount != 2 {
		t.Errorf("Expected 2 buckets, got %d", initialCount)
	}

	// Cleanup with max age of 0 should remove all buckets
	limiter.Cleanup(0)

	limiter.mu.Lock()
	finalCount := len(limiter.buckets)
	limiter.mu.Unlock()

	if finalCount != 0 {
		t.Errorf("Expected 0 buckets after cleanup, got %d", finalCount)
	}
}

func TestRateLimiter_NilConfig(t *testing.T) {
	limiter := NewRateLimiter(nil)

	// Should use default config
	if limiter.config == nil {
		t.Error("Expected default config to be set")
	}
	if !limiter.config.Enabled {
		t.Error("Default config should have Enabled=true")
	}
	if limiter.config.MessagesPerMinute != 20 {
		t.Errorf("Expected MessagesPerMinute=20, got %d", limiter.config.MessagesPerMinute)
	}
}

func TestDefaultRateLimitConfig(t *testing.T) {
	config := DefaultRateLimitConfig()

	if !config.Enabled {
		t.Error("Default config should be enabled")
	}
	if config.MessagesPerMinute != 20 {
		t.Errorf("Expected MessagesPerMinute=20, got %d", config.MessagesPerMinute)
	}
	if config.TasksPerHour != 10 {
		t.Errorf("Expected TasksPerHour=10, got %d", config.TasksPerHour)
	}
	if config.BurstSize != 5 {
		t.Errorf("Expected BurstSize=5, got %d", config.BurstSize)
	}
}
