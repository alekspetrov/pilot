package executor

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"
)

// TemplateData holds variables for template rendering
type TemplateData struct {
	// Task fields
	ID                 string
	TaskID             string // Alias for ID
	TaskTitle          string
	TaskSlug           string
	Title              string
	Description        string
	AcceptanceCriteria []string

	// Project fields
	ProjectName string
	TechStack   string

	// SOP fields
	Category string

	// Date fields
	Date     string // YYYY-MM-DD
	DateTime string // YYYY-MM-DD HH:MM
	Year     string

	// Custom fields
	Custom map[string]string
}

// TemplateRenderer renders Go templates
type TemplateRenderer struct {
	templatesDir string
	funcMap      template.FuncMap
}

// NewTemplateRenderer creates a renderer with template directory
func NewTemplateRenderer(templatesDir string) *TemplateRenderer {
	return &TemplateRenderer{
		templatesDir: templatesDir,
		funcMap: template.FuncMap{
			"date":     func() string { return time.Now().Format("2006-01-02") },
			"datetime": func() string { return time.Now().Format("2006-01-02 15:04") },
			"year":     func() string { return time.Now().Format("2006") },
			"lower":    strings.ToLower,
			"upper":    strings.ToUpper,
			"slugify":  slugify,
		},
	}
}

// RenderTemplate renders a template with data
func (r *TemplateRenderer) RenderTemplate(templateName string, data *TemplateData) (string, error) {
	// Find template file
	templatePath := filepath.Join(r.templatesDir, templateName)
	content, err := os.ReadFile(templatePath)
	if err != nil {
		return "", err
	}

	// Parse and execute
	tmpl, err := template.New(templateName).Funcs(r.funcMap).Parse(string(content))
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// RenderTaskDoc renders task template with task data
func (r *TemplateRenderer) RenderTaskDoc(taskID, title, description string, acceptanceCriteria []string) (string, error) {
	data := &TemplateData{
		ID:                 taskID,
		TaskID:             taskID,
		TaskTitle:          title,
		Title:              title,
		TaskSlug:           slugify(title),
		Description:        description,
		AcceptanceCriteria: acceptanceCriteria,
		Date:               time.Now().Format("2006-01-02"),
		DateTime:           time.Now().Format("2006-01-02 15:04"),
		Year:               time.Now().Format("2006"),
	}
	return r.RenderTemplate("task.md", data)
}

// RenderSOP renders SOP template
func (r *TemplateRenderer) RenderSOP(title, category string) (string, error) {
	data := &TemplateData{
		Title:    title,
		Category: category,
		Date:     time.Now().Format("2006-01-02"),
		DateTime: time.Now().Format("2006-01-02 15:04"),
		Year:     time.Now().Format("2006"),
		Custom:   map[string]string{"category": category},
	}
	return r.RenderTemplate("sop.md", data)
}

// RenderString renders a template string directly (not from file)
func (r *TemplateRenderer) RenderString(templateStr string, data *TemplateData) (string, error) {
	tmpl, err := template.New("inline").Funcs(r.funcMap).Parse(templateStr)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

func slugify(s string) string {
	// Replace spaces and underscores with hyphens
	result := strings.ReplaceAll(s, " ", "-")
	result = strings.ReplaceAll(result, "_", "-")
	// Convert to lowercase
	result = strings.ToLower(result)
	// Remove consecutive hyphens
	for strings.Contains(result, "--") {
		result = strings.ReplaceAll(result, "--", "-")
	}
	// Trim leading/trailing hyphens
	result = strings.Trim(result, "-")
	return result
}
