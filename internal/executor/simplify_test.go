package executor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultSimplifyConfig(t *testing.T) {
	config := DefaultSimplifyConfig()

	if !config.Enabled {
		t.Error("Expected Enabled to be true")
	}
	if config.Trigger != "post-implementation" {
		t.Errorf("Expected Trigger to be 'post-implementation', got %s", config.Trigger)
	}
	if config.Scope != "modified" {
		t.Errorf("Expected Scope to be 'modified', got %s", config.Scope)
	}
	if config.MaxFileSize != 50000 {
		t.Errorf("Expected MaxFileSize to be 50000, got %d", config.MaxFileSize)
	}
	if !config.PreserveComments {
		t.Error("Expected PreserveComments to be true")
	}
	if len(config.SkipPatterns) == 0 {
		t.Error("Expected SkipPatterns to be non-empty")
	}
}

func TestDefaultRules(t *testing.T) {
	rules := DefaultRules()

	if len(rules) < 3 {
		t.Errorf("Expected at least 3 rules, got %d", len(rules))
	}

	expectedRules := []string{
		"remove_redundant_else",
		"simplify_boolean_return",
		"remove_unnecessary_nil_check",
	}

	for _, expected := range expectedRules {
		found := false
		for _, rule := range rules {
			if rule.Name == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected rule %s not found", expected)
		}
	}
}

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"main.go", "go"},
		{"app.js", "js"},
		{"component.tsx", "ts"},
		{"script.py", "py"},
		{"Main.java", "java"},
		{"lib.rs", "rust"},
		{"README.md", ""},
		{"config.yaml", ""},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := detectLanguage(tt.path)
			if result != tt.expected {
				t.Errorf("detectLanguage(%s) = %s, want %s", tt.path, result, tt.expected)
			}
		})
	}
}

func TestIsCodeFile(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"main.go", true},
		{"app.js", true},
		{"component.tsx", true},
		{"README.md", false},
		{"config.yaml", false},
		{"data.json", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := isCodeFile(tt.path)
			if result != tt.expected {
				t.Errorf("isCodeFile(%s) = %v, want %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestCheckRedundantElse(t *testing.T) {
	tests := []struct {
		name     string
		code     string
		expected bool
	}{
		{
			name:     "has redundant else",
			code:     "if (x) {\n  return 1;\n} else {\n  x++;\n}",
			expected: true,
		},
		{
			name:     "no redundant else",
			code:     "if (x) {\n  x++;\n} else {\n  x--;\n}",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := checkRedundantElse(tt.code)
			if result != tt.expected {
				t.Errorf("checkRedundantElse() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestCheckBooleanReturn(t *testing.T) {
	tests := []struct {
		name     string
		code     string
		expected bool
	}{
		{
			name:     "has boolean return pattern",
			code:     "if (x > 0) { return true; } return false;",
			expected: true,
		},
		{
			name:     "no boolean return pattern",
			code:     "if (x > 0) { return x; } return 0;",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := checkBooleanReturn(tt.code)
			if result != tt.expected {
				t.Errorf("checkBooleanReturn() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestCheckUnnecessaryNilCheck(t *testing.T) {
	tests := []struct {
		name     string
		code     string
		expected bool
	}{
		{
			name:     "has unnecessary nil check",
			code:     "if err != nil { return err }",
			expected: true,
		},
		{
			name:     "has nil check with extra logic",
			code:     "if err != nil { log.Error(err); return err }",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := checkUnnecessaryNilCheck(tt.code)
			if result != tt.expected {
				t.Errorf("checkUnnecessaryNilCheck() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestSimplifyFile_SkipPattern(t *testing.T) {
	// Create temp file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "main_test.go")
	if err := os.WriteFile(testFile, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	config := &SimplifyConfig{
		Enabled:      true,
		SkipPatterns: []string{"*_test.go"},
		MaxFileSize:  50000,
	}

	simplifier := NewCodeSimplifier(config, nil)
	result := simplifier.SimplifyFile(testFile)

	if result.Changed {
		t.Error("Expected file to be skipped due to pattern")
	}
	if len(result.RulesApplied) > 0 {
		t.Error("Expected no rules applied for skipped file")
	}
}

func TestSimplifyFile_MaxSize(t *testing.T) {
	// Create large temp file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "large.go")

	// Create content larger than max size
	largeContent := make([]byte, 100)
	for i := range largeContent {
		largeContent[i] = 'x'
	}

	if err := os.WriteFile(testFile, largeContent, 0644); err != nil {
		t.Fatal(err)
	}

	config := &SimplifyConfig{
		Enabled:      true,
		SkipPatterns: []string{},
		MaxFileSize:  50, // Very small max
	}

	simplifier := NewCodeSimplifier(config, nil)
	result := simplifier.SimplifyFile(testFile)

	if result.Changed {
		t.Error("Expected file to be skipped due to size")
	}
}

func TestSimplifyFile_ApplyRules(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "main.go")

	code := `package main

func check(x int) bool {
	if (x > 0) { return true; } return false;
}
`

	if err := os.WriteFile(testFile, []byte(code), 0644); err != nil {
		t.Fatal(err)
	}

	config := DefaultSimplifyConfig()
	config.SkipPatterns = []string{} // Don't skip anything

	simplifier := NewCodeSimplifier(config, nil)
	result := simplifier.SimplifyFile(testFile)

	if result.Error != nil {
		t.Fatalf("Unexpected error: %v", result.Error)
	}

	// Should have applied the boolean return rule
	if !result.Changed {
		t.Error("Expected file to be changed")
	}

	found := false
	for _, rule := range result.RulesApplied {
		if rule == "simplify_boolean_return" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected simplify_boolean_return rule to be applied, got: %v", result.RulesApplied)
	}
}

func TestSimplifyModifiedFiles_Disabled(t *testing.T) {
	config := &SimplifyConfig{
		Enabled: false,
	}

	simplifier := NewCodeSimplifier(config, nil)
	results, err := simplifier.SimplifyModifiedFiles(context.Background(), t.TempDir())

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if results != nil {
		t.Error("Expected nil results when disabled")
	}
}

func TestContainsLanguage(t *testing.T) {
	languages := []string{"go", "js", "ts"}

	if !containsLanguage(languages, "go") {
		t.Error("Expected 'go' to be in languages")
	}
	if containsLanguage(languages, "python") {
		t.Error("Expected 'python' not to be in languages")
	}
	if containsLanguage(nil, "go") {
		t.Error("Expected false for nil languages")
	}
}

func TestNewCodeSimplifier_NilConfig(t *testing.T) {
	simplifier := NewCodeSimplifier(nil, nil)

	if simplifier.config == nil {
		t.Error("Expected default config when nil is passed")
	}
	if !simplifier.config.Enabled {
		t.Error("Expected default config to have Enabled=true")
	}
}

func TestApplySimplifyBooleanReturn(t *testing.T) {
	input := "if (x > 0) { return true; } return false;"
	result := applySimplifyBooleanReturn(input)

	if result == input {
		t.Error("Expected transformation to occur")
	}
	if result != "return x > 0" {
		t.Errorf("Expected 'return x > 0', got '%s'", result)
	}
}

func TestApplyRemoveUnnecessaryNilCheck(t *testing.T) {
	input := "if err != nil { return err }"
	result := applyRemoveUnnecessaryNilCheck(input)

	if result != "return err" {
		t.Errorf("Expected 'return err', got '%s'", result)
	}
}
