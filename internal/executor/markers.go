// Package executor provides task execution with Navigator integration.
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
		return fmt.Errorf("failed to create markers directory: %w", err)
	}

	// Generate filename: YYYY-MM-DD-HHMM_description.md
	timestamp := time.Now().Format("2006-01-02-1504")
	safeName := sanitizeMarkerName(marker.Description)
	if safeName == "" {
		safeName = "marker"
	}
	filename := fmt.Sprintf("%s_%s.md", timestamp, safeName)

	marker.FilePath = filepath.Join(markersDir, filename)
	marker.CreatedAt = time.Now()

	content := formatMarker(marker)
	if err := os.WriteFile(marker.FilePath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write marker file: %w", err)
	}

	// Update .active file
	return SetActiveMarker(agentPath, marker.FilePath)
}

// SetActiveMarker updates the .active file to point to current marker.
func SetActiveMarker(agentPath, markerPath string) error {
	markersDir := filepath.Join(agentPath, ".context-markers")
	if err := os.MkdirAll(markersDir, 0755); err != nil {
		return fmt.Errorf("failed to create markers directory: %w", err)
	}

	activePath := filepath.Join(markersDir, ".active")
	if err := os.WriteFile(activePath, []byte(markerPath), 0644); err != nil {
		return fmt.Errorf("failed to write active marker: %w", err)
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
		return "", fmt.Errorf("failed to read active marker: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

// LoadMarker reads and parses a marker file.
func LoadMarker(path string) (*ContextMarker, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read marker file: %w", err)
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
		return nil, fmt.Errorf("failed to read markers directory: %w", err)
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
	for _, m := range markers {
		if m.CreatedAt.Before(cutoff) {
			if err := os.Remove(m.FilePath); err != nil && !os.IsNotExist(err) {
				// Log but don't fail on individual file removal errors
				continue
			}
		}
	}
	return nil
}

// formatMarker converts a ContextMarker to markdown content.
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
		sb.WriteString("_No focus set_")
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
	}

	return sb.String()
}

// parseMarker parses markdown content into a ContextMarker.
func parseMarker(content, path string) (*ContextMarker, error) {
	marker := &ContextMarker{FilePath: path}

	// Extract description from title
	titleRe := regexp.MustCompile(`(?m)^# Context Marker: (.+)$`)
	if match := titleRe.FindStringSubmatch(content); len(match) > 1 {
		marker.Description = strings.TrimSpace(match[1])
	}

	// Extract created date
	createdRe := regexp.MustCompile(`\*\*Created:\*\* (\d{4}-\d{2}-\d{2} \d{2}:\d{2})`)
	if match := createdRe.FindStringSubmatch(content); len(match) > 1 {
		if t, err := time.Parse("2006-01-02 15:04", match[1]); err == nil {
			marker.CreatedAt = t
		}
	}

	// If no created date found, try to parse from filename
	if marker.CreatedAt.IsZero() {
		filename := filepath.Base(path)
		filenameRe := regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}-\d{4})_`)
		if match := filenameRe.FindStringSubmatch(filename); len(match) > 1 {
			if t, err := time.Parse("2006-01-02-1504", match[1]); err == nil {
				marker.CreatedAt = t
			}
		}
	}

	// Extract task ID
	taskRe := regexp.MustCompile(`\*\*Task:\*\* (.+)`)
	if match := taskRe.FindStringSubmatch(content); len(match) > 1 {
		marker.TaskID = strings.TrimSpace(match[1])
	}

	// Extract current focus (text after "## Current Focus" until next "##" or end)
	focusRe := regexp.MustCompile(`(?s)## Current Focus\n\n(.+?)(?:\n## |$)`)
	if match := focusRe.FindStringSubmatch(content); len(match) > 1 {
		focus := strings.TrimSpace(match[1])
		if focus != "_No focus set_" {
			marker.CurrentFocus = focus
		}
	}

	// Extract files modified
	marker.FilesModified = parseListSection(content, "## Files Modified")

	// Extract commits
	marker.Commits = parseListSection(content, "## Commits")

	// Generate name from description or filename
	if marker.Description != "" {
		marker.Name = marker.Description
	} else {
		marker.Name = strings.TrimSuffix(filepath.Base(path), ".md")
	}

	return marker, nil
}

// parseListSection extracts bullet list items from a markdown section.
func parseListSection(content, sectionHeader string) []string {
	idx := strings.Index(content, sectionHeader)
	if idx == -1 {
		return nil
	}

	// Find section content
	sectionStart := idx + len(sectionHeader)
	sectionEnd := len(content)

	// Find next section header
	nextSection := strings.Index(content[sectionStart:], "\n## ")
	if nextSection != -1 {
		sectionEnd = sectionStart + nextSection
	}

	sectionContent := content[sectionStart:sectionEnd]

	// Parse bullet items
	var items []string
	scanner := bufio.NewScanner(strings.NewReader(sectionContent))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "- ") {
			items = append(items, strings.TrimPrefix(line, "- "))
		}
	}

	return items
}

// sanitizeMarkerName converts a description into a safe filename component.
func sanitizeMarkerName(description string) string {
	// Convert to lowercase and replace spaces with hyphens
	result := strings.ToLower(description)
	result = strings.ReplaceAll(result, " ", "-")

	// Keep only alphanumeric characters and hyphens
	var safe []byte
	for i := 0; i < len(result); i++ {
		c := result[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			safe = append(safe, c)
		}
	}

	// Collapse multiple hyphens
	result = string(safe)
	for strings.Contains(result, "--") {
		result = strings.ReplaceAll(result, "--", "-")
	}

	// Trim leading/trailing hyphens and limit length
	result = strings.Trim(result, "-")
	if len(result) > 50 {
		result = result[:50]
		// Don't end with a hyphen
		result = strings.TrimRight(result, "-")
	}

	return result
}
