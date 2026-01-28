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

// NavigatorIndexSync handles updating the Navigator DEVELOPMENT-README.md index
// when tasks are completed. It moves completed tasks from "In Progress" to "Completed"
// and updates status markers.
type NavigatorIndexSync struct {
	projectPath string
}

// NewNavigatorIndexSync creates a new NavigatorIndexSync for the given project path.
func NewNavigatorIndexSync(projectPath string) *NavigatorIndexSync {
	return &NavigatorIndexSync{projectPath: projectPath}
}

// indexPath returns the path to DEVELOPMENT-README.md
func (n *NavigatorIndexSync) indexPath() string {
	return filepath.Join(n.projectPath, ".agent", "DEVELOPMENT-README.md")
}

// HasNavigator checks if the project has Navigator initialized
func (n *NavigatorIndexSync) HasNavigator() bool {
	_, err := os.Stat(n.indexPath())
	return err == nil
}

// SyncTaskCompleted updates the Navigator index when a task completes.
// It moves the task from "In Progress" to "Completed" section.
func (n *NavigatorIndexSync) SyncTaskCompleted(taskID string) error {
	if !n.HasNavigator() {
		return nil // No Navigator, nothing to sync
	}

	content, err := os.ReadFile(n.indexPath())
	if err != nil {
		return fmt.Errorf("read navigator index: %w", err)
	}

	updated, changed := n.updateIndexContent(string(content), taskID)
	if !changed {
		return nil // Task not found in index or already in Completed
	}

	if err := os.WriteFile(n.indexPath(), []byte(updated), 0644); err != nil {
		return fmt.Errorf("write navigator index: %w", err)
	}

	return nil
}

// updateIndexContent performs the actual content transformation.
// It finds the task in "In Progress" and moves it to "Completed".
// Returns the updated content and whether any changes were made.
func (n *NavigatorIndexSync) updateIndexContent(content, taskID string) (string, bool) {
	lines := strings.Split(content, "\n")
	var result []string
	var changed bool

	// Normalize task ID for matching (support both TASK-XX and GH-XX formats)
	taskNum := extractTaskNumber(taskID)
	if taskNum == "" {
		taskNum = taskID // Use as-is if no number found
	}

	// Track section state
	inProgressSection := false
	completedSection := false
	completedTableFound := false
	taskEntry := ""
	taskTitle := ""

	// First pass: find and remove task from "In Progress"
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Detect sections
		if strings.HasPrefix(trimmed, "### In Progress") {
			inProgressSection = true
			completedSection = false
			result = append(result, line)
			continue
		}
		if strings.HasPrefix(trimmed, "### Backlog") || strings.HasPrefix(trimmed, "---") {
			if inProgressSection {
				inProgressSection = false
			}
		}
		if strings.HasPrefix(trimmed, "## Completed") {
			inProgressSection = false
			completedSection = true
			result = append(result, line)
			continue
		}

		// Check for task in "In Progress" table
		if inProgressSection && strings.HasPrefix(trimmed, "|") {
			// Skip header rows
			if strings.Contains(trimmed, "GH#") || strings.Contains(trimmed, "TASK#") ||
				strings.Contains(trimmed, "---") || strings.Contains(trimmed, "Title") {
				result = append(result, line)
				continue
			}

			// Check if this row matches our task
			if matchesTask(trimmed, taskID, taskNum) {
				// Extract title for Completed section
				taskTitle = extractTitleFromRow(trimmed)
				taskEntry = taskID
				changed = true
				// Skip this line (remove from In Progress)
				continue
			}
		}

		// Track Completed table location for insertion
		if completedSection && strings.HasPrefix(trimmed, "|") && !completedTableFound {
			if strings.Contains(trimmed, "Item") && strings.Contains(trimmed, "What") {
				completedTableFound = true
				result = append(result, line)
				continue
			}
			if strings.Contains(trimmed, "---") && completedTableFound {
				result = append(result, line)
				// Insert completed task entry after separator
				if taskEntry != "" && taskTitle != "" {
					newRow := fmt.Sprintf("| %s | %s |", taskEntry, taskTitle)
					result = append(result, newRow)
				}
				continue
			}
		}

		result = append(result, line)
	}

	// If task wasn't inserted in Completed section (maybe section format different),
	// try to add it at a reasonable location
	if changed && taskEntry != "" && taskTitle != "" && !containsCompletedEntry(result, taskEntry) {
		result = insertCompletedEntry(result, taskEntry, taskTitle)
	}

	return strings.Join(result, "\n"), changed
}

// extractTaskNumber extracts the numeric part from task IDs like "TASK-32", "GH-57"
func extractTaskNumber(taskID string) string {
	re := regexp.MustCompile(`(?i)(?:TASK|GH|LIN)-?(\d+)`)
	matches := re.FindStringSubmatch(taskID)
	if len(matches) >= 2 {
		return matches[1]
	}
	return ""
}

// matchesTask checks if a table row matches the given task ID
func matchesTask(row, taskID, taskNum string) bool {
	row = strings.ToLower(row)
	taskIDLower := strings.ToLower(taskID)

	// Direct match
	if strings.Contains(row, taskIDLower) {
		return true
	}

	// Number-only match in first column (for rows like "| 57 | Title |")
	if taskNum != "" {
		parts := strings.Split(row, "|")
		if len(parts) >= 2 {
			firstCol := strings.TrimSpace(parts[1])
			if firstCol == taskNum {
				return true
			}
		}
	}

	return false
}

// extractTitleFromRow extracts the title from a markdown table row
func extractTitleFromRow(row string) string {
	parts := strings.Split(row, "|")
	if len(parts) >= 3 {
		return strings.TrimSpace(parts[2])
	}
	return "Task completed"
}

// containsCompletedEntry checks if the result already has an entry for the task
func containsCompletedEntry(lines []string, taskID string) bool {
	taskIDLower := strings.ToLower(taskID)
	for _, line := range lines {
		if strings.Contains(strings.ToLower(line), taskIDLower) {
			return true
		}
	}
	return false
}

// insertCompletedEntry adds a completed entry to the Completed section
func insertCompletedEntry(lines []string, taskID, title string) []string {
	var result []string

	for i, line := range lines {
		result = append(result, line)
		trimmed := strings.TrimSpace(line)

		// Look for "## Completed" section and insert after the table header
		if strings.HasPrefix(trimmed, "## Completed") {
			// Find the table and insert after header separator
			for j := i + 1; j < len(lines) && j < i+10; j++ {
				nextLine := strings.TrimSpace(lines[j])
				result = append(result, lines[j])
				// Found separator line (|---|---|)
				if strings.Contains(nextLine, "|") && strings.Contains(nextLine, "---") {
					// Insert new entry
					newRow := fmt.Sprintf("| %s | %s |", taskID, title)
					result = append(result, newRow)
					// Continue from j+1
					for k := j + 1; k < len(lines); k++ {
						result = append(result, lines[k])
					}
					return result
				}
			}
		}
	}

	// If Completed section not found, append at end
	result = append(result, "")
	result = append(result, fmt.Sprintf("## Completed (%s)", time.Now().Format("2006-01-02")))
	result = append(result, "")
	result = append(result, "| Item | What |")
	result = append(result, "|------|------|")
	result = append(result, fmt.Sprintf("| %s | %s |", taskID, title))

	return result
}

// UpdateTaskStatus updates a specific task's status in the Navigator index.
// This is a more general method that can change status markers.
func (n *NavigatorIndexSync) UpdateTaskStatus(taskID, newStatus string) error {
	if !n.HasNavigator() {
		return nil
	}

	content, err := os.ReadFile(n.indexPath())
	if err != nil {
		return fmt.Errorf("read navigator index: %w", err)
	}

	// Status emoji mapping
	statusEmoji := map[string]string{
		"completed":   "✅",
		"in_progress": "🔄",
		"failed":      "❌",
		"pending":     "⏳",
	}

	emoji, ok := statusEmoji[strings.ToLower(newStatus)]
	if !ok {
		emoji = "🔄"
	}

	// Find and update status in the task row
	lines := strings.Split(string(content), "\n")
	taskNum := extractTaskNumber(taskID)
	changed := false

	for i, line := range lines {
		if matchesTask(line, taskID, taskNum) && strings.HasPrefix(strings.TrimSpace(line), "|") {
			// Update status column (typically last column with emoji)
			parts := strings.Split(line, "|")
			if len(parts) >= 4 {
				// Status is typically in the last data column
				statusIdx := len(parts) - 2
				parts[statusIdx] = fmt.Sprintf(" %s Completed ", emoji)
				lines[i] = strings.Join(parts, "|")
				changed = true
				break
			}
		}
	}

	if !changed {
		return nil
	}

	return os.WriteFile(n.indexPath(), []byte(strings.Join(lines, "\n")), 0644)
}

// ParseTaskFileStatus reads a task file and extracts its status.
// Task files are expected to have a status marker like "Status: Completed"
func ParseTaskFileStatus(taskFilePath string) (string, error) {
	file, err := os.Open(taskFilePath)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	statusRe := regexp.MustCompile(`(?i)^\*?\*?status\*?\*?:?\s*(.+)$`)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if matches := statusRe.FindStringSubmatch(line); len(matches) >= 2 {
			return strings.TrimSpace(matches[1]), nil
		}
	}

	return "", nil // No status found
}
