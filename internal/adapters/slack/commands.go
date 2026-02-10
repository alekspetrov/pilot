package slack

import (
	"strings"
)

// Command represents a parsed text command
type Command struct {
	// Name is the command name (e.g., "help", "status", "switch")
	Name string
	// Args is the remaining text after the command name
	Args string
}

// ParseCommand parses a text command from user input.
// Commands start with "/" (e.g., "/help", "/status", "/switch project-name")
func ParseCommand(text string) *Command {
	text = strings.TrimSpace(text)

	// Remove leading slash if present
	text = strings.TrimPrefix(text, "/")

	if text == "" {
		return nil
	}

	// Split into command and args
	parts := strings.SplitN(text, " ", 2)
	cmd := &Command{
		Name: strings.ToLower(parts[0]),
	}

	if len(parts) > 1 {
		cmd.Args = strings.TrimSpace(parts[1])
	}

	return cmd
}

// formatHelpMessage returns the help message with available commands
func formatHelpMessage() string {
	return `:robot_face: *Pilot Commands*

*Modes*
:speech_balloon: *Chat* — Just talk to me for opinions and discussions
:mag: *Questions* — Ask about the codebase ("What files handle auth?")
:microscope: *Research* — Deep analysis ("Research how caching works")
:triangular_ruler: *Planning* — Design before building ("Plan how to add rate limiting")
:rocket: *Tasks* — Build features ("Add a logout button")

*Commands*
` + "`/help`" + ` — Show this message
` + "`/status`" + ` — Show current project and task status
` + "`/queue`" + ` — Show pending and running tasks
` + "`/switch <project>`" + ` — Switch to a different project
` + "`/cancel [task-id]`" + ` — Cancel a running task

*Tips*
• In channels, mention me (@Pilot) to get my attention
• In DMs, just type your message
• I'll ask for confirmation before making code changes`
}

// IsCommand checks if a text starts with a command prefix
func IsCommand(text string) bool {
	text = strings.TrimSpace(text)
	return strings.HasPrefix(text, "/")
}

// KnownCommands returns the list of known command names
func KnownCommands() []string {
	return []string{"help", "status", "queue", "switch", "cancel"}
}
