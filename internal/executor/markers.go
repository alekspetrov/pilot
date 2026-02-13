package executor

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ContextMarker represents a saved session checkpoint
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

// CreateMarker saves a context marker to .agent/.context-markers/
func CreateMarker(agentPath string, marker *ContextMarker) error {
	markersDir := filepath.Join(agentPath, ".context-markers")
	if err := os.MkdirAll(markersDir, 0755); err != nil {
		return err
	}

	// Generate filename: YYYY-MM-DD-HHMM_description.md
	timestamp := time.Now().Format("2006-01-02-1504")
	safeName := sanitizeFilename(marker.Description)
	filename := fmt.Sprintf("%s_%s.md", timestamp, safeName)

	marker.FilePath = filepath.Join(markersDir, filename)
	marker.CreatedAt = time.Now()

	content := formatMarker(marker)
	if err := os.WriteFile(marker.FilePath, []byte(content), 0644); err != nil {
		return err
	}

	// Update .active file
	return SetActiveMarker(agentPath, marker.FilePath)
}

// SetActiveMarker updates the .active file to point to current marker
func SetActiveMarker(agentPath, markerPath string) error {
	activePath := filepath.Join(agentPath, ".context-markers", ".active")
	return os.WriteFile(activePath, []byte(markerPath), 0644)
}

// GetActiveMarker returns the currently active marker path
func GetActiveMarker(agentPath string) (string, error) {
	activePath := filepath.Join(agentPath, ".context-markers", ".active")
	data, err := os.ReadFile(activePath)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// LoadMarker reads and parses a marker file
func LoadMarker(path string) (*ContextMarker, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseMarker(string(data), path)
}

// ListMarkers returns all markers sorted by date (newest first)
func ListMarkers(agentPath string) ([]*ContextMarker, error) {
	markersDir := filepath.Join(agentPath, ".context-markers")
	entries, err := os.ReadDir(markersDir)
	if err != nil {
		return nil, err
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

// CleanupOldMarkers removes markers older than retention period
func CleanupOldMarkers(agentPath string, retentionDays int) error {
	markers, err := ListMarkers(agentPath)
	if err != nil {
		return err
	}

	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	for _, m := range markers {
		if m.CreatedAt.Before(cutoff) {
			os.Remove(m.FilePath)
		}
	}
	return nil
}

func formatMarker(m *ContextMarker) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Context Marker: %s\n\n", m.Description))
	sb.WriteString(fmt.Sprintf("**Created:** %s\n\n", m.CreatedAt.Format("2006-01-02 15:04")))

	if m.TaskID != "" {
		sb.WriteString(fmt.Sprintf("**Task:** %s\n\n", m.TaskID))
	}

	sb.WriteString("## Current Focus\n\n")
	sb.WriteString(m.CurrentFocus)
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

func parseMarker(content, path string) (*ContextMarker, error) {
	marker := &ContextMarker{FilePath: path}

	// Extract description from title: # Context Marker: <description>
	titleRe := regexp.MustCompile(`(?m)^# Context Marker: (.+)$`)
	if matches := titleRe.FindStringSubmatch(content); len(matches) > 1 {
		marker.Description = strings.TrimSpace(matches[1])
	}

	// Extract created date: **Created:** YYYY-MM-DD HH:MM
	createdRe := regexp.MustCompile(`\*\*Created:\*\* (\d{4}-\d{2}-\d{2} \d{2}:\d{2})`)
	if matches := createdRe.FindStringSubmatch(content); len(matches) > 1 {
		if t, err := time.Parse("2006-01-02 15:04", matches[1]); err == nil {
			marker.CreatedAt = t
		}
	}

	// Extract task ID: **Task:** <task>
	taskRe := regexp.MustCompile(`\*\*Task:\*\* (.+)`)
	if matches := taskRe.FindStringSubmatch(content); len(matches) > 1 {
		marker.TaskID = strings.TrimSpace(matches[1])
	}

	// Extract current focus section
	focusRe := regexp.MustCompile(`(?s)## Current Focus\n\n(.+?)(?:\n\n##|\z)`)
	if matches := focusRe.FindStringSubmatch(content); len(matches) > 1 {
		marker.CurrentFocus = strings.TrimSpace(matches[1])
	}

	// Extract files modified - stop at next section or end
	filesRe := regexp.MustCompile(`(?m)## Files Modified\n\n((?:- [^\n]+\n)+)`)
	if matches := filesRe.FindStringSubmatch(content); len(matches) > 1 {
		lines := strings.Split(strings.TrimSpace(matches[1]), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "- ") {
				marker.FilesModified = append(marker.FilesModified, strings.TrimPrefix(line, "- "))
			}
		}
	}

	// Extract commits - stop at next section or end
	commitsRe := regexp.MustCompile(`(?m)## Commits\n\n((?:- [^\n]+\n?)+)`)
	if matches := commitsRe.FindStringSubmatch(content); len(matches) > 1 {
		lines := strings.Split(strings.TrimSpace(matches[1]), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "- ") {
				marker.Commits = append(marker.Commits, strings.TrimPrefix(line, "- "))
			}
		}
	}

	// Derive name from filename
	marker.Name = filepath.Base(path)

	return marker, nil
}

// sanitizeFilename makes a string safe for use in filenames
func sanitizeFilename(s string) string {
	// Replace spaces with hyphens
	s = strings.ReplaceAll(s, " ", "-")
	// Remove or replace unsafe characters
	s = regexp.MustCompile(`[^a-zA-Z0-9\-_]`).ReplaceAllString(s, "")
	// Limit length
	if len(s) > 50 {
		s = s[:50]
	}
	// Lowercase
	return strings.ToLower(s)
}
