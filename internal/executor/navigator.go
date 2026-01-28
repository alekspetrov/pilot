package executor

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// navigatorIndexPath returns the path to DEVELOPMENT-README.md for a project.
func navigatorIndexPath(projectPath string) string {
	return filepath.Join(projectPath, ".agent", "DEVELOPMENT-README.md")
}

// SyncNavigatorIndex updates the Navigator index after task completion.
// It moves the task from "In Progress" to "Completed" section.
// Supports both TASK-XX and GH-XX formats.
func (r *Runner) SyncNavigatorIndex(task *Task, status string) error {
	indexPath := navigatorIndexPath(task.ProjectPath)

	// Check if Navigator index exists
	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		r.log.Debug("Navigator index not found, skipping sync",
			"task_id", task.ID,
			"path", indexPath,
		)
		return nil // Not an error - project may not use Navigator
	}

	// Read current index
	content, err := os.ReadFile(indexPath)
	if err != nil {
		return fmt.Errorf("failed to read Navigator index: %w", err)
	}

	// Parse task ID to extract number and prefix
	taskID := task.ID
	taskNum, taskPrefix := parseTaskID(taskID)
	if taskNum == "" {
		r.log.Debug("Could not parse task ID format, skipping sync",
			"task_id", taskID,
		)
		return nil
	}

	// Update content
	updated, changed := updateNavigatorIndex(string(content), taskID, taskNum, taskPrefix, task.Title, status)
	if !changed {
		r.log.Debug("No changes needed in Navigator index",
			"task_id", taskID,
		)
		return nil
	}

	// Write updated content
	if err := os.WriteFile(indexPath, []byte(updated), 0644); err != nil {
		return fmt.Errorf("failed to write Navigator index: %w", err)
	}

	r.log.Info("Navigator index updated",
		"task_id", taskID,
		"status", status,
		"path", indexPath,
	)

	return nil
}

// parseTaskID extracts number and prefix from task ID.
// Returns (number, prefix) or ("", "") if not parseable.
// Examples:
//   - "TASK-123" -> ("123", "TASK")
//   - "GH-45" -> ("45", "GH")
//   - "LIN-789" -> ("789", "LIN")
func parseTaskID(taskID string) (string, string) {
	// Pattern: PREFIX-NUMBER or PREFIX_NUMBER
	re := regexp.MustCompile(`^([A-Z]+)[-_](\d+)$`)
	matches := re.FindStringSubmatch(strings.ToUpper(taskID))
	if len(matches) == 3 {
		return matches[2], matches[1]
	}
	return "", ""
}

// updateNavigatorIndex modifies the index content to reflect task completion.
// Returns (updatedContent, wasChanged).
func updateNavigatorIndex(content, taskID, taskNum, taskPrefix, taskTitle, status string) (string, bool) {
	lines := strings.Split(content, "\n")
	var result []string
	changed := false

	// Track section positions
	inProgressStart := -1
	inProgressEnd := -1
	completedStart := -1
	// Find "In Progress" and "Completed" sections
	for i, line := range lines {
		lineLower := strings.ToLower(strings.TrimSpace(line))

		// Find "### In Progress" section
		if strings.HasPrefix(lineLower, "### in progress") {
			inProgressStart = i
		}

		// Find end of In Progress table (next header or empty section)
		if inProgressStart != -1 && inProgressEnd == -1 && i > inProgressStart+2 {
			// Check if we hit a new section or significant gap
			if strings.HasPrefix(line, "##") || strings.HasPrefix(line, "---") {
				inProgressEnd = i
			}
		}

		// Find "## Completed" section (various formats)
		if strings.HasPrefix(lineLower, "## completed") {
			completedStart = i
		}
	}

	// If we didn't find end markers, set them to reasonable defaults
	if inProgressStart != -1 && inProgressEnd == -1 {
		inProgressEnd = len(lines)
	}

	// Process: remove from In Progress, add to Completed
	taskEntry := ""
	removedLine := -1

	// First pass: find and remove task from In Progress
	for i, line := range lines {
		// Check if this line contains our task
		if i > inProgressStart && i < inProgressEnd && containsTaskID(line, taskNum, taskPrefix) {
			// Found the task - capture its info and mark for removal
			taskEntry = extractTaskEntry(line, taskNum, taskPrefix, taskTitle)
			removedLine = i
			changed = true
			continue
		}
		result = append(result, line)
	}

	// If we removed a task and have a Completed section, add it there
	if changed && completedStart != -1 && taskEntry != "" {
		// Adjust completedStart since we removed a line
		if removedLine < completedStart {
			completedStart--
		}

		// Find where to insert in Completed section (after table header)
		insertPos := -1
		for i := completedStart; i < len(result) && i < completedStart+5; i++ {
			if strings.HasPrefix(strings.TrimSpace(result[i]), "|") &&
				strings.Contains(strings.ToLower(result[i]), "item") {
				// Found table header row
				if i+1 < len(result) && strings.Contains(result[i+1], "---") {
					insertPos = i + 2 // After header and separator
					break
				}
			}
		}

		if insertPos != -1 && insertPos <= len(result) {
			// Build completion entry
			today := time.Now().Format("2006-01-02")
			completionEntry := fmt.Sprintf("| %s | %s (completed %s) |", taskID, taskEntry, today)

			// Insert the new entry
			newResult := make([]string, 0, len(result)+1)
			newResult = append(newResult, result[:insertPos]...)
			newResult = append(newResult, completionEntry)
			newResult = append(newResult, result[insertPos:]...)
			result = newResult
		}
	}

	// If we didn't find a Completed section but did remove from In Progress,
	// the change still counts (task was removed from active)
	if !changed {
		return content, false
	}

	return strings.Join(result, "\n"), true
}

// containsTaskID checks if a line contains the specified task ID.
func containsTaskID(line, taskNum, taskPrefix string) bool {
	// Skip non-table lines
	if !strings.Contains(line, "|") {
		return false
	}

	// Skip header/separator rows
	if strings.Contains(line, "---") || strings.Contains(strings.ToLower(line), "title") ||
		strings.Contains(strings.ToLower(line), "status") {
		return false
	}

	lineLower := strings.ToLower(line)
	taskNumLower := strings.ToLower(taskNum)

	// Check for various formats:
	// | 54 | ... (just number)
	// | GH-54 | ... (full ID)
	// | GH# | ... with 54 in the row
	patterns := []string{
		fmt.Sprintf("| %s |", taskNum),       // | 54 |
		fmt.Sprintf("|%s|", taskNum),         // |54|
		fmt.Sprintf("| %s |", taskNumLower),  // | 54 | (lowercase check)
		fmt.Sprintf("%s-%s", taskPrefix, taskNum), // GH-54 or TASK-123
	}

	for _, pattern := range patterns {
		if strings.Contains(lineLower, strings.ToLower(pattern)) {
			return true
		}
	}

	// Also check if line starts with the number after first |
	parts := strings.Split(line, "|")
	if len(parts) >= 2 {
		firstCell := strings.TrimSpace(parts[1])
		if firstCell == taskNum || firstCell == fmt.Sprintf("%s-%s", taskPrefix, taskNum) {
			return true
		}
	}

	return false
}

// extractTaskEntry extracts the task title/description from a table row.
func extractTaskEntry(line, taskNum, taskPrefix, defaultTitle string) string {
	parts := strings.Split(line, "|")
	if len(parts) >= 3 {
		// Get the second column (title/description)
		title := strings.TrimSpace(parts[2])
		if title != "" && !strings.Contains(strings.ToLower(title), "status") {
			return title
		}
	}

	// Fall back to provided title
	if defaultTitle != "" {
		return defaultTitle
	}

	return fmt.Sprintf("%s-%s", taskPrefix, taskNum)
}

// UpdateTaskStatus updates a task's status in the Navigator index.
// This is a lower-level function that can update any status field.
func UpdateTaskStatus(projectPath, taskID, newStatus string) error {
	indexPath := navigatorIndexPath(projectPath)

	content, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No Navigator index
		}
		return err
	}

	// Simple status replacement in the row containing taskID
	lines := strings.Split(string(content), "\n")
	changed := false

	taskNum, taskPrefix := parseTaskID(taskID)
	if taskNum == "" {
		return nil
	}

	for i, line := range lines {
		if containsTaskID(line, taskNum, taskPrefix) {
			// Replace status emoji/text
			oldStatuses := []string{"🔄 Pilot executing", "⏳ Queued", "🔴 Blocked"}
			newStatusText := statusToEmoji(newStatus)

			for _, old := range oldStatuses {
				if strings.Contains(line, old) {
					lines[i] = strings.Replace(line, old, newStatusText, 1)
					changed = true
					break
				}
			}
		}
	}

	if changed {
		return os.WriteFile(indexPath, []byte(strings.Join(lines, "\n")), 0644)
	}

	return nil
}

// statusToEmoji converts a status string to its emoji representation.
func statusToEmoji(status string) string {
	switch strings.ToLower(status) {
	case "completed", "done", "success":
		return "✅ Completed"
	case "failed", "error":
		return "❌ Failed"
	case "in_progress", "running", "executing":
		return "🔄 Pilot executing"
	case "queued", "pending":
		return "⏳ Queued"
	case "blocked":
		return "🔴 Blocked"
	default:
		return status
	}
}

// GetNavigatorTasks returns all tasks from the Navigator index.
// Useful for status reporting and dashboards.
func GetNavigatorTasks(projectPath string) ([]NavigatorTask, error) {
	indexPath := navigatorIndexPath(projectPath)

	file, err := os.Open(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = file.Close() }()

	var tasks []NavigatorTask
	scanner := bufio.NewScanner(file)
	inTaskSection := false

	for scanner.Scan() {
		line := scanner.Text()
		lineLower := strings.ToLower(strings.TrimSpace(line))

		// Track sections
		if strings.HasPrefix(lineLower, "### in progress") ||
			strings.HasPrefix(lineLower, "### backlog") ||
			strings.HasPrefix(lineLower, "## completed") {
			inTaskSection = true
			continue
		}

		if inTaskSection && strings.HasPrefix(line, "##") && !strings.HasPrefix(lineLower, "## completed") {
			inTaskSection = false
			continue
		}

		// Parse table rows
		if inTaskSection && strings.HasPrefix(strings.TrimSpace(line), "|") {
			task := parseTaskRow(line)
			if task.ID != "" {
				tasks = append(tasks, task)
			}
		}
	}

	return tasks, scanner.Err()
}

// NavigatorTask represents a task entry from the Navigator index.
type NavigatorTask struct {
	ID          string
	Title       string
	Status      string
	Section     string // "in_progress", "backlog", "completed"
}

// parseTaskRow parses a markdown table row into a NavigatorTask.
func parseTaskRow(line string) NavigatorTask {
	// Skip header/separator rows
	if strings.Contains(line, "---") || strings.Contains(strings.ToLower(line), "gh#") ||
		strings.Contains(strings.ToLower(line), "task#") || strings.Contains(strings.ToLower(line), "title") {
		return NavigatorTask{}
	}

	parts := strings.Split(line, "|")
	if len(parts) < 3 {
		return NavigatorTask{}
	}

	id := strings.TrimSpace(parts[1])
	title := ""
	status := ""

	if len(parts) >= 3 {
		title = strings.TrimSpace(parts[2])
	}
	if len(parts) >= 4 {
		status = strings.TrimSpace(parts[3])
	}

	// Skip if ID looks like a header
	if id == "" || strings.ToLower(id) == "item" || strings.ToLower(id) == "priority" {
		return NavigatorTask{}
	}

	return NavigatorTask{
		ID:     id,
		Title:  title,
		Status: status,
	}
}
