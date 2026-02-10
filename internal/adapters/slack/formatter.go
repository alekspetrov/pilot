package slack

import (
	"fmt"
	"strings"
	"time"

	"github.com/alekspetrov/pilot/internal/executor"
)

const (
	// slackBlockTextLimit is the maximum character limit for block text in Slack
	slackBlockTextLimit = 3000
)

// FormatGreeting returns welcome blocks with mode descriptions for Slack.
// Mirrors the telegram/formatter.go:FormatGreeting format.
func FormatGreeting(username string) []Block {
	name := "there"
	if username != "" {
		name = username
	}

	headerText := fmt.Sprintf(":wave: Hey %s! I'm Pilot.", name)

	modesText := `:speech_balloon: *Chat* — Ask opinions or discuss
"What do you think about using Redis?"

:mag: *Questions* — Quick answers
"What files handle auth?"

:microscope: *Research* — Deep analysis
"Research how caching works here"

:triangular_ruler: *Planning* — Design before building
"Plan how to add rate limiting"

:rocket: *Tasks* — Build features
"Add a logout button"

Type /help for commands.`

	return []Block{
		{
			Type: "header",
			Text: &TextObject{
				Type:  "plain_text",
				Text:  headerText,
				Emoji: true,
			},
		},
		{
			Type: "section",
			Text: &TextObject{
				Type: "mrkdwn",
				Text: modesText,
			},
		},
	}
}

// FormatTaskConfirmation returns blocks for task confirmation with Execute/Cancel buttons.
func FormatTaskConfirmation(taskID, description string) []interface{} {
	truncatedDesc := truncateDescription(description, 200)

	blocks := []interface{}{
		Block{
			Type: "header",
			Text: &TextObject{
				Type:  "plain_text",
				Text:  ":clipboard: Confirm Task",
				Emoji: true,
			},
		},
		Block{
			Type: "section",
			Text: &TextObject{
				Type: "mrkdwn",
				Text: fmt.Sprintf("*Task ID:* %s\n\n%s", taskID, truncatedDesc),
			},
		},
		Block{
			Type: "divider",
		},
		ActionsBlock{
			Type:    "actions",
			BlockID: fmt.Sprintf("task_confirm_%s", taskID),
			Elements: []ButtonElement{
				{
					Type: "button",
					Text: &TextObject{
						Type:  "plain_text",
						Text:  "Execute",
						Emoji: true,
					},
					ActionID: "execute_task",
					Value:    taskID,
					Style:    "primary",
				},
				{
					Type: "button",
					Text: &TextObject{
						Type:  "plain_text",
						Text:  "Cancel",
						Emoji: true,
					},
					ActionID: "cancel_task",
					Value:    taskID,
					Style:    "danger",
				},
			},
		},
	}

	return blocks
}

// FormatPlanWithActions returns blocks for a plan with Execute/Cancel buttons.
func FormatPlanWithActions(taskID, planSummary string) []interface{} {
	// Truncate plan summary for display
	displaySummary := planSummary
	if len(displaySummary) > 2500 {
		displaySummary = displaySummary[:2500] + "\n\n_(truncated)_"
	}

	blocks := []interface{}{
		Block{
			Type: "header",
			Text: &TextObject{
				Type:  "plain_text",
				Text:  ":clipboard: Implementation Plan",
				Emoji: true,
			},
		},
		Block{
			Type: "section",
			Text: &TextObject{
				Type: "mrkdwn",
				Text: displaySummary,
			},
		},
		Block{
			Type: "divider",
		},
		Block{
			Type: "section",
			Text: &TextObject{
				Type: "mrkdwn",
				Text: "*Ready to execute this plan?*",
			},
		},
		ActionsBlock{
			Type:    "actions",
			BlockID: fmt.Sprintf("plan_confirm_%s", taskID),
			Elements: []ButtonElement{
				{
					Type: "button",
					Text: &TextObject{
						Type:  "plain_text",
						Text:  "Execute Plan",
						Emoji: true,
					},
					ActionID: "execute_plan",
					Value:    taskID,
					Style:    "primary",
				},
				{
					Type: "button",
					Text: &TextObject{
						Type:  "plain_text",
						Text:  "Cancel",
						Emoji: true,
					},
					ActionID: "cancel_plan",
					Value:    taskID,
					Style:    "danger",
				},
			},
		},
	}

	return blocks
}

// FormatProgressUpdate returns blocks for a progress update.
func FormatProgressUpdate(phase string, progress int, elapsed time.Duration) []Block {
	// Clamp progress
	if progress < 0 {
		progress = 0
	}
	if progress > 100 {
		progress = 100
	}

	// Build progress bar (20 chars)
	filled := progress / 5
	bar := strings.Repeat("█", filled) + strings.Repeat("░", 20-filled)

	// Phase emoji
	phaseEmoji := ":hourglass_flowing_sand:"
	switch phase {
	case "Starting":
		phaseEmoji = ":rocket:"
	case "Branching":
		phaseEmoji = ":seedling:"
	case "Exploring":
		phaseEmoji = ":mag:"
	case "Installing":
		phaseEmoji = ":package:"
	case "Implementing":
		phaseEmoji = ":gear:"
	case "Testing":
		phaseEmoji = ":test_tube:"
	case "Committing":
		phaseEmoji = ":floppy_disk:"
	case "Completed":
		phaseEmoji = ":white_check_mark:"
	case "Navigator":
		phaseEmoji = ":compass:"
	}

	elapsedStr := elapsed.Round(time.Second).String()

	return []Block{
		{
			Type: "section",
			Text: &TextObject{
				Type: "mrkdwn",
				Text: fmt.Sprintf("%s *%s* (%d%%)\n`%s`\n\n:stopwatch: Elapsed: %s",
					phaseEmoji, phase, progress, bar, elapsedStr),
			},
		},
	}
}

// FormatTaskResult returns blocks for a task result (success or failure).
func FormatTaskResult(result *executor.ExecutionResult) []Block {
	if result.Success {
		return formatSuccessBlocks(result)
	}
	return formatFailureBlocks(result)
}

// formatSuccessBlocks formats a successful task result as Slack blocks.
func formatSuccessBlocks(result *executor.ExecutionResult) []Block {
	var blocks []Block

	// Header
	blocks = append(blocks, Block{
		Type: "header",
		Text: &TextObject{
			Type:  "plain_text",
			Text:  ":white_check_mark: Task Completed",
			Emoji: true,
		},
	})

	// Task ID and duration
	infoText := fmt.Sprintf("*Task:* %s\n*Duration:* %s",
		result.TaskID,
		result.Duration.Round(time.Second).String())

	// Add commit SHA if present
	if result.CommitSHA != "" {
		shortSHA := result.CommitSHA
		if len(shortSHA) > 8 {
			shortSHA = shortSHA[:8]
		}
		infoText += fmt.Sprintf("\n*Commit:* `%s`", shortSHA)
	}

	blocks = append(blocks, Block{
		Type: "section",
		Text: &TextObject{
			Type: "mrkdwn",
			Text: infoText,
		},
	})

	// Quality gates summary if available
	if result.QualityGates != nil && result.QualityGates.Enabled && len(result.QualityGates.Gates) > 0 {
		blocks = append(blocks, formatQualityGatesBlock(result.QualityGates))
	}

	// PR link if available
	if result.PRUrl != "" {
		blocks = append(blocks, Block{
			Type: "section",
			Text: &TextObject{
				Type: "mrkdwn",
				Text: fmt.Sprintf(":link: *Pull Request:* <%s|View PR>", result.PRUrl),
			},
		})
	}

	return blocks
}

// formatFailureBlocks formats a failed task result as Slack blocks.
func formatFailureBlocks(result *executor.ExecutionResult) []Block {
	var blocks []Block

	// Header
	blocks = append(blocks, Block{
		Type: "header",
		Text: &TextObject{
			Type:  "plain_text",
			Text:  ":x: Task Failed",
			Emoji: true,
		},
	})

	// Task ID and duration
	infoText := fmt.Sprintf("*Task:* %s\n*Duration:* %s",
		result.TaskID,
		result.Duration.Round(time.Second).String())

	blocks = append(blocks, Block{
		Type: "section",
		Text: &TextObject{
			Type: "mrkdwn",
			Text: infoText,
		},
	})

	// Quality gates summary if available
	if result.QualityGates != nil && result.QualityGates.Enabled && len(result.QualityGates.Gates) > 0 {
		blocks = append(blocks, formatQualityGatesBlock(result.QualityGates))
	}

	// Error message
	errorText := result.Error
	if errorText == "" {
		errorText = "Unknown error"
	}

	// Chunk error if too long
	chunks := chunkContent(errorText, slackBlockTextLimit-100) // Leave room for formatting
	for i, chunk := range chunks {
		label := ""
		if i == 0 {
			label = "*Error:*\n"
		}
		blocks = append(blocks, Block{
			Type: "section",
			Text: &TextObject{
				Type: "mrkdwn",
				Text: label + "```" + chunk + "```",
			},
		})
	}

	return blocks
}

// formatQualityGatesBlock formats quality gate results as a Slack block.
func formatQualityGatesBlock(qg *executor.QualityGatesResult) Block {
	var sb strings.Builder

	// Count passed gates
	passed := 0
	for _, g := range qg.Gates {
		if g.Passed {
			passed++
		}
	}
	sb.WriteString(fmt.Sprintf(":lock: *Quality Gates:* %d/%d passed\n", passed, len(qg.Gates)))

	// List individual gates
	for _, gate := range qg.Gates {
		var icon string
		if gate.Passed {
			icon = ":white_check_mark:"
		} else {
			icon = ":x:"
		}

		durationStr := gate.Duration.Round(time.Second).String()
		sb.WriteString(fmt.Sprintf("• %s %s (%s", gate.Name, icon, durationStr))

		if gate.RetryCount > 0 {
			sb.WriteString(fmt.Sprintf(", %d retry", gate.RetryCount))
		}
		sb.WriteString(")\n")
	}

	return Block{
		Type: "section",
		Text: &TextObject{
			Type: "mrkdwn",
			Text: sb.String(),
		},
	}
}

// FormatQuestionAnswer returns a mrkdwn section block for an answer.
func FormatQuestionAnswer(answer string) []Block {
	// Chunk if too long
	chunks := chunkContent(answer, slackBlockTextLimit)

	blocks := make([]Block, 0, len(chunks))
	for _, chunk := range chunks {
		blocks = append(blocks, Block{
			Type: "section",
			Text: &TextObject{
				Type: "mrkdwn",
				Text: chunk,
			},
		})
	}

	return blocks
}

// chunkContent splits content into chunks of maxLen characters.
// Tries to break at newlines for cleaner output.
func chunkContent(content string, maxLen int) []string {
	if maxLen <= 0 {
		maxLen = slackBlockTextLimit
	}

	if len(content) <= maxLen {
		return []string{content}
	}

	var chunks []string
	remaining := content

	for len(remaining) > 0 {
		if len(remaining) <= maxLen {
			chunks = append(chunks, remaining)
			break
		}

		// Find a good break point (prefer newline)
		breakPoint := maxLen
		if idx := strings.LastIndex(remaining[:maxLen], "\n"); idx > maxLen/2 {
			breakPoint = idx + 1
		}

		chunks = append(chunks, strings.TrimSpace(remaining[:breakPoint]))
		remaining = strings.TrimSpace(remaining[breakPoint:])
	}

	return chunks
}

// truncateDescription truncates a string to maxLen.
func truncateDescription(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
