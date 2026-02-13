// Package executor provides context marker management for Navigator sessions.
package executor

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

// ContextMarker represents a saved session checkpoint.
type ContextMarker struct {
	Name        string
	Description string
	CreatedAt   time.Time
	FilePath    string

	// Session state
	TaskID        string
	FilesModified []string
	Commits       []string
	CurrentFocus  string
}

// CreateMarker saves a context marker to .agent/.context-markers/.
func CreateMarker(agentPath string, marker *ContextMarker) error {
	markersDir := filepath.Join(agentPath, ".context-markers")
	if err := os.MkdirAll(markersDir, 0755); err != nil {
		return fmt.Errorf("create markers directory: %w", err)
	}

	// Generate filename: YYYY-MM-DD-HHMM_description.md
	now := time.Now()
	timestamp := now.Format("2006-01-02-1504")
	safeName := sanitizeMarkerFilename(marker.Description)
	if safeName == "" {
		safeName = "checkpoint"
	}
	filename := fmt.Sprintf("%s_%s.md", timestamp, safeName)

	marker.FilePath = filepath.Join(markersDir, filename)
	marker.CreatedAt = now

	content := formatMarker(marker)
	if err := os.WriteFile(marker.FilePath, []byte(content), 0644); err != nil {
		return fmt.Errorf("write marker file: %w", err)
	}

	// Update .active file
	return SetActiveMarker(agentPath, marker.FilePath)
}

// SetActiveMarker updates the .active file to point to current marker.
func SetActiveMarker(agentPath, markerPath string) error {
	markersDir := filepath.Join(agentPath, ".context-markers")
	if err := os.MkdirAll(markersDir, 0755); err != nil {
		return fmt.Errorf("create markers directory: %w", err)
	}

	activePath := filepath.Join(markersDir, ".active")
	if err := os.WriteFile(activePath, []byte(markerPath), 0644); err != nil {
		return fmt.Errorf("write active marker: %w", err)
	}
	return nil
}

// GetActiveMarker returns the currently active marker path.
func GetActiveMarker(agentPath string) (string, error) {
	activePath := filepath.Join(agentPath, ".context-markers", ".active")
	data, err := os.ReadFile(activePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil // No active marker is not an error
		}
		return "", fmt.Errorf("read active marker: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

// LoadMarker reads and parses a marker file.
func LoadMarker(path string) (*ContextMarker, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read marker file: %w", err)
	}
	return parseMarker(string(data), path)
}

// ListMarkers returns all markers sorted by date (newest first).
func ListMarkers(agentPath string) ([]*ContextMarker, error) {
	markersDir := filepath.Join(agentPath, ".context-markers")
	entries, err := os.ReadDir(markersDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No markers directory is not an error
		}
		return nil, fmt.Errorf("read markers directory: %w", err)
	}

	var markers []*ContextMarker
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(markersDir, entry.Name())
		marker, err := LoadMarker(path)
		if err != nil {
			continue // skip invalid markers
		}
		markers = append(markers, marker)
	}

	sort.Slice(markers, func(i, j int) bool {
		return markers[i].CreatedAt.After(markers[j].CreatedAt)
	})

	return markers, nil
}

// CleanupOldMarkers removes markers older than retention period.
func CleanupOldMarkers(agentPath string, retentionDays int) error {
	markers, err := ListMarkers(agentPath)
	if err != nil {
		return err
	}

	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	var removeErrors []error

	for _, m := range markers {
		if m.CreatedAt.Before(cutoff) {
			if err := os.Remove(m.FilePath); err != nil && !os.IsNotExist(err) {
				removeErrors = append(removeErrors, err)
			}
		}
	}

	if len(removeErrors) > 0 {
		return fmt.Errorf("failed to remove %d marker(s)", len(removeErrors))
	}
	return nil
}

// formatMarker generates markdown content for a marker.
func formatMarker(m *ContextMarker) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Context Marker: %s\n\n", m.Description))
	sb.WriteString(fmt.Sprintf("**Created:** %s\n\n", m.CreatedAt.Format("2006-01-02 15:04")))

	if m.TaskID != "" {
		sb.WriteString(fmt.Sprintf("**Task:** %s\n\n", m.TaskID))
	}

	sb.WriteString("## Current Focus\n\n")
	if m.CurrentFocus != "" {
		sb.WriteString(m.CurrentFocus)
	} else {
		sb.WriteString("(No focus specified)")
	}
	sb.WriteString("\n\n")

	if len(m.FilesModified) > 0 {
		sb.WriteString("## Files Modified\n\n")
		for _, f := range m.FilesModified {
			sb.WriteString(fmt.Sprintf("- %s\n", f))
		}
		sb.WriteString("\n")
	}

	if len(m.Commits) > 0 {
		sb.WriteString("## Commits\n\n")
		for _, c := range m.Commits {
			sb.WriteString(fmt.Sprintf("- %s\n", c))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// parseMarker extracts marker fields from markdown content.
func parseMarker(content, path string) (*ContextMarker, error) {
	marker := &ContextMarker{
		FilePath: path,
		Name:     filepath.Base(path),
	}

	scanner := bufio.NewScanner(strings.NewReader(content))
	var currentSection string
	var focusLines []string

	// Regex patterns for extracting values
	titleRe := regexp.MustCompile(`^#\s+Context Marker:\s*(.+)$`)
	createdRe := regexp.MustCompile(`^\*\*Created:\*\*\s*(.+)$`)
	taskRe := regexp.MustCompile(`^\*\*Task:\*\*\s*(.+)$`)
	listItemRe := regexp.MustCompile(`^-\s+(.+)$`)

	for scanner.Scan() {
		line := scanner.Text()

		// Check for section headers
		if strings.HasPrefix(line, "## ") {
			// Save focus content if we were in focus section
			if currentSection == "focus" && len(focusLines) > 0 {
				marker.CurrentFocus = strings.TrimSpace(strings.Join(focusLines, "\n"))
			}
			focusLines = nil

			section := strings.TrimPrefix(line, "## ")
			switch {
			case strings.HasPrefix(section, "Current Focus"):
				currentSection = "focus"
			case strings.HasPrefix(section, "Files Modified"):
				currentSection = "files"
			case strings.HasPrefix(section, "Commits"):
				currentSection = "commits"
			default:
				currentSection = ""
			}
			continue
		}

		// Parse title
		if match := titleRe.FindStringSubmatch(line); match != nil {
			marker.Description = strings.TrimSpace(match[1])
			continue
		}

		// Parse created date
		if match := createdRe.FindStringSubmatch(line); match != nil {
			dateStr := strings.TrimSpace(match[1])
			if t, err := time.Parse("2006-01-02 15:04", dateStr); err == nil {
				marker.CreatedAt = t
			}
			continue
		}

		// Parse task
		if match := taskRe.FindStringSubmatch(line); match != nil {
			marker.TaskID = strings.TrimSpace(match[1])
			continue
		}

		// Process list items based on current section
		if match := listItemRe.FindStringSubmatch(line); match != nil {
			item := strings.TrimSpace(match[1])
			switch currentSection {
			case "files":
				marker.FilesModified = append(marker.FilesModified, item)
			case "commits":
				marker.Commits = append(marker.Commits, item)
			}
			continue
		}

		// Collect focus content
		if currentSection == "focus" && strings.TrimSpace(line) != "" {
			focusLines = append(focusLines, line)
		}
	}

	// Handle focus section at end of file
	if currentSection == "focus" && len(focusLines) > 0 {
		marker.CurrentFocus = strings.TrimSpace(strings.Join(focusLines, "\n"))
	}

	// If CreatedAt wasn't parsed, try to extract from filename
	if marker.CreatedAt.IsZero() {
		marker.CreatedAt = parseTimestampFromFilename(filepath.Base(path))
	}

	return marker, nil
}

// parseTimestampFromFilename extracts timestamp from YYYY-MM-DD-HHMM format.
func parseTimestampFromFilename(filename string) time.Time {
	// Format: 2006-01-02-1504_description.md
	if len(filename) < 15 {
		return time.Time{}
	}

	timestampPart := filename[:15] // "2006-01-02-1504"
	t, err := time.Parse("2006-01-02-1504", timestampPart)
	if err != nil {
		return time.Time{}
	}
	return t
}

// sanitizeMarkerFilename creates a safe filename from description.
func sanitizeMarkerFilename(s string) string {
	var result strings.Builder
	lastWasUnderscore := false

	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			result.WriteRune(r)
			lastWasUnderscore = false
		} else if !lastWasUnderscore && result.Len() > 0 {
			result.WriteRune('_')
			lastWasUnderscore = true
		}
	}

	str := result.String()
	// Trim trailing underscore
	str = strings.TrimSuffix(str, "_")
	// Limit length
	if len(str) > 50 {
		str = str[:50]
	}
	return str
}
