package executor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSlugify(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Hello World", "hello-world"},
		{"Test_With_Underscores", "test-with-underscores"},
		{"Multiple   Spaces", "multiple-spaces"},
		{"Already-Slugified", "already-slugified"},
		{"  Leading Trailing  ", "leading-trailing"},
		{"MixedCase", "mixedcase"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := slugify(tt.input)
			if result != tt.expected {
				t.Errorf("slugify(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestNewTemplateRenderer(t *testing.T) {
	r := NewTemplateRenderer("/tmp/templates")
	if r == nil {
		t.Fatal("NewTemplateRenderer returned nil")
	}
	if r.templatesDir != "/tmp/templates" {
		t.Errorf("templatesDir = %q, want %q", r.templatesDir, "/tmp/templates")
	}
	if r.funcMap == nil {
		t.Error("funcMap is nil")
	}
}

func TestRenderString(t *testing.T) {
	r := NewTemplateRenderer("")

	data := &TemplateData{
		TaskID:    "TASK-01",
		TaskTitle: "Test Task",
		TaskSlug:  "test-task",
	}

	tests := []struct {
		name     string
		template string
		want     string
	}{
		{
			name:     "simple variable",
			template: "ID: {{.TaskID}}",
			want:     "ID: TASK-01",
		},
		{
			name:     "multiple variables",
			template: "{{.TaskID}}: {{.TaskTitle}}",
			want:     "TASK-01: Test Task",
		},
		{
			name:     "with slugify function",
			template: "{{slugify .TaskTitle}}",
			want:     "test-task",
		},
		{
			name:     "with lower function",
			template: "{{lower .TaskTitle}}",
			want:     "test task",
		},
		{
			name:     "with upper function",
			template: "{{upper .TaskTitle}}",
			want:     "TEST TASK",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := r.RenderString(tt.template, data)
			if err != nil {
				t.Fatalf("RenderString error: %v", err)
			}
			if result != tt.want {
				t.Errorf("RenderString() = %q, want %q", result, tt.want)
			}
		})
	}
}

func TestRenderTemplate(t *testing.T) {
	// Create temp directory with test template
	tmpDir, err := os.MkdirTemp("", "templates-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Write test template
	templateContent := `# {{.TaskID}}: {{.TaskTitle}}
Date: {{.Date}}
Slug: {{slugify .TaskTitle}}`

	err = os.WriteFile(filepath.Join(tmpDir, "test.md"), []byte(templateContent), 0644)
	if err != nil {
		t.Fatal(err)
	}

	r := NewTemplateRenderer(tmpDir)
	data := &TemplateData{
		TaskID:    "TASK-99",
		TaskTitle: "My Test Feature",
		Date:      "2026-02-13",
	}

	result, err := r.RenderTemplate("test.md", data)
	if err != nil {
		t.Fatalf("RenderTemplate error: %v", err)
	}

	expected := `# TASK-99: My Test Feature
Date: 2026-02-13
Slug: my-test-feature`

	if result != expected {
		t.Errorf("RenderTemplate() =\n%s\nwant:\n%s", result, expected)
	}
}

func TestRenderTaskDoc(t *testing.T) {
	// Create temp directory with task template
	tmpDir, err := os.MkdirTemp("", "templates-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Write task template
	templateContent := `# {{.ID}}
Title: {{.Title}}
{{range .AcceptanceCriteria}}
- [ ] {{.}}
{{end}}`

	err = os.WriteFile(filepath.Join(tmpDir, "task.md"), []byte(templateContent), 0644)
	if err != nil {
		t.Fatal(err)
	}

	r := NewTemplateRenderer(tmpDir)
	result, err := r.RenderTaskDoc("TASK-01", "Add Feature", "Description here", []string{"Criterion 1", "Criterion 2"})
	if err != nil {
		t.Fatalf("RenderTaskDoc error: %v", err)
	}

	if !strings.Contains(result, "TASK-01") {
		t.Error("result missing TaskID")
	}
	if !strings.Contains(result, "Add Feature") {
		t.Error("result missing Title")
	}
	if !strings.Contains(result, "Criterion 1") {
		t.Error("result missing acceptance criteria")
	}
}

func TestRenderSOP(t *testing.T) {
	// Create temp directory with SOP template
	tmpDir, err := os.MkdirTemp("", "templates-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Write SOP template
	templateContent := `# SOP: {{.Title}}
Category: {{.Category}}
Date: {{.Date}}`

	err = os.WriteFile(filepath.Join(tmpDir, "sop.md"), []byte(templateContent), 0644)
	if err != nil {
		t.Fatal(err)
	}

	r := NewTemplateRenderer(tmpDir)
	result, err := r.RenderSOP("Deploy Process", "Operations")
	if err != nil {
		t.Fatalf("RenderSOP error: %v", err)
	}

	if !strings.Contains(result, "Deploy Process") {
		t.Error("result missing Title")
	}
	if !strings.Contains(result, "Operations") {
		t.Error("result missing Category")
	}
}

func TestRenderTemplate_NotFound(t *testing.T) {
	r := NewTemplateRenderer("/nonexistent")
	_, err := r.RenderTemplate("missing.md", &TemplateData{})
	if err == nil {
		t.Error("expected error for missing template")
	}
}

func TestRenderString_InvalidTemplate(t *testing.T) {
	r := NewTemplateRenderer("")
	_, err := r.RenderString("{{.Invalid", &TemplateData{})
	if err == nil {
		t.Error("expected error for invalid template syntax")
	}
}
