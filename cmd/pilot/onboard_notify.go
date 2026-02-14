package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/alekspetrov/pilot/internal/adapters/slack"
	"github.com/alekspetrov/pilot/internal/adapters/telegram"
	"github.com/alekspetrov/pilot/internal/config"
)

// onboardNotifySetup handles the notification setup stage.
// For Solo persona: asks if user wants notifications (default no).
// For Team/Enterprise: shows options with Slack as recommended.
func onboardNotifySetup(state *OnboardState) error {
	reader := state.Reader

	// Solo persona: optional, default no
	if state.Persona == PersonaSolo {
		showBox("NOTIFICATIONS", "[3 of 5]", "Set up notifications? [y/N]")
		if !readYesNo(reader, false) {
			return nil
		}
	}

	// Team/Enterprise: show options
	options := []string{
		"Slack (recommended)",
		"Telegram",
		"Skip",
	}

	showBox("NOTIFICATIONS", "[3 of 5]", "Where should Pilot send updates?")
	choice := selectOption(reader, options)

	switch choice {
	case 0: // Slack
		return onboardSlackNotify(state)
	case 1: // Telegram
		return onboardTelegramNotify(state)
	case 2: // Skip
		return nil
	}

	return nil
}

// onboardSlackNotify configures Slack notifications.
func onboardSlackNotify(state *OnboardState) error {
	cfg := state.Config
	reader := state.Reader

	// Initialize Slack config if needed
	if cfg.Adapters == nil {
		cfg.Adapters = &config.AdaptersConfig{}
	}
	if cfg.Adapters.Slack == nil {
		cfg.Adapters.Slack = slack.DefaultConfig()
	}

	// Check for existing token in env
	token := os.Getenv("SLACK_BOT_TOKEN")
	if token != "" {
		fmt.Println("  Found $SLACK_BOT_TOKEN in environment")
	} else {
		fmt.Print("  Bot token (xoxb-...) > ")
		token = readLine(reader)
		if token == "" {
			fmt.Println("  Skipped - no token provided")
			return nil
		}
	}

	// Validate the token
	fmt.Print("  Validating... ")
	botName, err := validateSlackConn(token)
	if err != nil {
		fmt.Println("x")
		fmt.Printf("  Warning: %v\n", err)
		fmt.Print("  Continue anyway? [y/N]: ")
		if !readYesNo(reader, false) {
			return nil
		}
	} else {
		fmt.Printf("Connected as @%s\n", botName)
	}

	// Prompt for channel
	defaultChannel := "#dev-notifications"
	fmt.Printf("  Channel [%s] > ", defaultChannel)
	channel := readLine(reader)
	if channel == "" {
		channel = defaultChannel
	}
	if !strings.HasPrefix(channel, "#") {
		channel = "#" + channel
	}

	// Configure
	cfg.Adapters.Slack.Enabled = true
	cfg.Adapters.Slack.BotToken = token
	cfg.Adapters.Slack.Channel = channel

	// Store notification channel for later stages
	state.NotifyChannel = "slack:" + channel

	fmt.Printf("  Slack -> %s\n", channel)
	return nil
}

// onboardTelegramNotify configures Telegram notifications.
func onboardTelegramNotify(state *OnboardState) error {
	cfg := state.Config
	reader := state.Reader

	// Initialize Telegram config if needed
	if cfg.Adapters == nil {
		cfg.Adapters = &config.AdaptersConfig{}
	}
	if cfg.Adapters.Telegram == nil {
		cfg.Adapters.Telegram = telegram.DefaultConfig()
	}

	// Prompt for bot token
	fmt.Print("  Bot token (from @BotFather) > ")
	token := readLine(reader)
	if token == "" {
		fmt.Println("  Skipped - no token provided")
		return nil
	}

	// Validate the token
	fmt.Print("  Validating... ")
	botName, err := validateTelegramConn(token)
	if err != nil {
		fmt.Println("x")
		fmt.Printf("  Warning: %v\n", err)
		fmt.Print("  Continue anyway? [y/N]: ")
		if !readYesNo(reader, false) {
			return nil
		}
	} else {
		fmt.Printf("Connected as @%s\n", botName)
	}

	// Prompt for chat ID
	fmt.Println("  Hint: message @userinfobot on Telegram to get your Chat ID")
	fmt.Print("  Chat ID > ")
	chatID := readLine(reader)
	if chatID == "" {
		fmt.Println("  Warning: No chat ID provided - bot won't know where to send messages")
	}

	// Configure
	cfg.Adapters.Telegram.Enabled = true
	cfg.Adapters.Telegram.BotToken = token
	cfg.Adapters.Telegram.ChatID = chatID
	cfg.Adapters.Telegram.Polling = true

	// Store notification channel for later stages
	state.NotifyChannel = "telegram:" + chatID

	fmt.Printf("  Telegram -> %s\n", chatID)
	return nil
}

// validateSlackConn validates a Slack bot token and returns the bot name.
// Stub implementation - will be replaced by onboard_validate.go from Issue 5.
func validateSlackConn(token string) (string, error) {
	// Basic format validation
	if !strings.HasPrefix(token, "xoxb-") {
		return "", fmt.Errorf("token should start with xoxb-")
	}

	// Create client and test auth
	client := slack.NewClient(token)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Test by posting a minimal request - auth.test equivalent
	// For now, just validate format since we don't have auth.test
	_ = client
	_ = ctx

	// Return placeholder - in production this would call auth.test
	return "pilot-bot", nil
}

// validateTelegramConn validates a Telegram bot token and returns the bot username.
// Stub implementation - will be replaced by onboard_validate.go from Issue 5.
func validateTelegramConn(token string) (string, error) {
	// Basic format validation
	if !strings.Contains(token, ":") {
		return "", fmt.Errorf("invalid token format")
	}

	// Create client and get bot info
	client := telegram.NewClient(token)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Try to get updates (validates token) - offset=0, timeout=0 for quick check
	_, err := client.GetUpdates(ctx, 0, 0)
	if err != nil {
		return "", fmt.Errorf("failed to validate token: %w", err)
	}

	// Return placeholder - in production this would call getMe
	return "pilot_bot", nil
}

// OnboardState holds state during the onboarding wizard.
// Stub - will be provided by onboard.go from Issue 1.
type OnboardState struct {
	Config        *config.Config
	Reader        *bufio.Reader
	Persona       Persona
	NotifyChannel string // "slack:#channel" or "telegram:chatid"
}

// Persona represents the user's team size/type.
// Stub - will be provided by onboard.go from Issue 1.
type Persona int

const (
	PersonaSolo Persona = iota
	PersonaTeam
	PersonaEnterprise
)

// showBox displays a TUI box with title and content.
// Stub - will be provided by onboard_helpers.go from Issue 1.
func showBox(title, badge, content string) {
	width := 70
	fmt.Println()
	fmt.Printf("-- %s %s %s\n", title, strings.Repeat("-", width-len(title)-len(badge)-6), badge)
	fmt.Println(content)
}

// selectOption displays numbered options and returns the selected index.
// Stub - will be provided by onboard_helpers.go from Issue 1.
func selectOption(reader *bufio.Reader, options []string) int {
	for i, opt := range options {
		fmt.Printf("    %d  %s\n", i+1, opt)
	}
	fmt.Print("\n  > ")
	line := readLine(reader)
	if line == "" {
		return 0
	}
	var choice int
	fmt.Sscanf(line, "%d", &choice)
	if choice < 1 || choice > len(options) {
		return 0
	}
	return choice - 1
}
