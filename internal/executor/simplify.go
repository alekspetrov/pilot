package executor

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// SimplifyConfig controls code simplification behavior.
type SimplifyConfig struct {
	Enabled          bool     `yaml:"enabled"`           // Enable simplification (default: true)
	Trigger          string   `yaml:"trigger"`           // "post-implementation" or "on-demand"
	Scope            string   `yaml:"scope"`             // "modified" or "all"
	SkipPatterns     []string `yaml:"skip_patterns"`     // Glob patterns to skip ["*.test.*", "*.md"]
	MaxFileSize      int      `yaml:"max_file_size"`     // Max file size in chars (default: 50000)
	PreserveComments bool     `yaml:"preserve_comments"` // Keep comments during simplification
}

// DefaultSimplifyConfig returns default simplification configuration.
func DefaultSimplifyConfig() *SimplifyConfig {
	return &SimplifyConfig{
		Enabled:          true,
		Trigger:          "post-implementation",
		Scope:            "modified",
		SkipPatterns:     []string{"*.test.*", "*.spec.*", "*_test.go", "*.md", "*.json", "*.yaml", "*.yml"},
		MaxFileSize:      50000,
		PreserveComments: true,
	}
}

// SimplificationRule defines a simplification pattern.
type SimplificationRule struct {
	Name        string
	Description string
	Languages   []string                       // Applicable languages (empty = all)
	Check       func(code string) bool         // Returns true if rule applies
	Apply       func(code string) string       // Transforms the code
}

// SimplificationResult contains the result of simplification.
type SimplificationResult struct {
	Path         string
	RulesApplied []string
	Changed      bool
	Error        error
}

// CodeSimplifier handles code simplification.
type CodeSimplifier struct {
	config *SimplifyConfig
	rules  []SimplificationRule
	log    *slog.Logger
}

// NewCodeSimplifier creates a new code simplifier.
func NewCodeSimplifier(config *SimplifyConfig, log *slog.Logger) *CodeSimplifier {
	if config == nil {
		config = DefaultSimplifyConfig()
	}
	if log == nil {
		log = slog.Default()
	}
	return &CodeSimplifier{
		config: config,
		rules:  DefaultRules(),
		log:    log,
	}
}

// DefaultRules returns standard simplification rules.
func DefaultRules() []SimplificationRule {
	return []SimplificationRule{
		{
			Name:        "remove_redundant_else",
			Description: "Remove else after return/throw/break/continue",
			Languages:   []string{"go", "js", "ts", "java", "py"},
			Check:       checkRedundantElse,
			Apply:       applyRemoveRedundantElse,
		},
		{
			Name:        "simplify_boolean_return",
			Description: "Simplify 'if (cond) return true; return false' to 'return cond'",
			Languages:   []string{"go", "js", "ts", "java"},
			Check:       checkBooleanReturn,
			Apply:       applySimplifyBooleanReturn,
		},
		{
			Name:        "remove_unnecessary_nil_check",
			Description: "Remove err != nil check before returning err",
			Languages:   []string{"go"},
			Check:       checkUnnecessaryNilCheck,
			Apply:       applyRemoveUnnecessaryNilCheck,
		},
	}
}

// Regex patterns for rule detection and transformation.
var (
	// Matches: return/throw/break/continue followed by else
	redundantElsePattern = regexp.MustCompile(`(?m)(return|throw|break|continue)[^}]*\}\s*else\s*\{`)

	// Matches: if (cond) { return true; } return false;
	booleanReturnPattern = regexp.MustCompile(`(?m)if\s*\([^)]+\)\s*\{\s*return\s+true;?\s*\}\s*return\s+false;?`)

	// Matches: if err != nil { return err }
	unnecessaryNilCheckPattern = regexp.MustCompile(`(?m)if\s+err\s*!=\s*nil\s*\{\s*return\s+err\s*\}`)
)

func checkRedundantElse(code string) bool {
	return redundantElsePattern.MatchString(code)
}

func applyRemoveRedundantElse(code string) string {
	// Find and remove redundant else blocks
	// Pattern: } else { ... } after return/throw/break/continue
	// This is a simplified transformation that handles common cases

	// Match: return ...\n} else {
	pattern := regexp.MustCompile(`(?m)(return[^}]*)\}\s*else\s*\{([^}]*)\}`)

	result := pattern.ReplaceAllStringFunc(code, func(match string) string {
		// Extract the return statement and else body
		parts := pattern.FindStringSubmatch(match)
		if len(parts) >= 3 {
			returnStmt := parts[1]
			elseBody := strings.TrimSpace(parts[2])
			// Keep the return, remove else wrapper
			return returnStmt + "}\n" + elseBody
		}
		return match
	})

	return result
}

func checkBooleanReturn(code string) bool {
	return booleanReturnPattern.MatchString(code)
}

func applySimplifyBooleanReturn(code string) string {
	// Replace: if (cond) { return true } return false
	// With: return cond

	pattern := regexp.MustCompile(`(?m)if\s*\(([^)]+)\)\s*\{\s*return\s+true;?\s*\}\s*return\s+false;?`)

	return pattern.ReplaceAllStringFunc(code, func(match string) string {
		parts := pattern.FindStringSubmatch(match)
		if len(parts) >= 2 {
			cond := strings.TrimSpace(parts[1])
			return "return " + cond
		}
		return match
	})
}

func checkUnnecessaryNilCheck(code string) bool {
	return unnecessaryNilCheckPattern.MatchString(code)
}

func applyRemoveUnnecessaryNilCheck(code string) string {
	// Replace: if err != nil { return err }
	// With: return err
	// Only when there's nothing else in the block

	return unnecessaryNilCheckPattern.ReplaceAllString(code, "return err")
}

// SimplifyFile applies rules to a single file.
func (s *CodeSimplifier) SimplifyFile(path string) *SimplificationResult {
	result := &SimplificationResult{
		Path:         path,
		RulesApplied: []string{},
		Changed:      false,
	}

	// Check skip patterns
	for _, pattern := range s.config.SkipPatterns {
		if matched, _ := filepath.Match(pattern, filepath.Base(path)); matched {
			s.log.Debug("Skipping file due to pattern",
				slog.String("path", path),
				slog.String("pattern", pattern),
			)
			return result
		}
	}

	// Read file
	content, err := os.ReadFile(path)
	if err != nil {
		result.Error = err
		return result
	}

	// Check max file size
	if len(content) > s.config.MaxFileSize {
		s.log.Debug("Skipping file due to size",
			slog.String("path", path),
			slog.Int("size", len(content)),
			slog.Int("max", s.config.MaxFileSize),
		)
		return result
	}

	code := string(content)
	originalCode := code

	// Detect language from extension
	lang := detectLanguage(path)

	// Apply each rule
	for _, rule := range s.rules {
		// Check if rule applies to this language
		if len(rule.Languages) > 0 && !containsLanguage(rule.Languages, lang) {
			continue
		}

		// Check if rule matches
		if rule.Check(code) {
			code = rule.Apply(code)
			result.RulesApplied = append(result.RulesApplied, rule.Name)
			s.log.Debug("Applied simplification rule",
				slog.String("path", path),
				slog.String("rule", rule.Name),
			)
		}
	}

	// Write if changed
	if code != originalCode {
		result.Changed = true
		if err := os.WriteFile(path, []byte(code), 0644); err != nil {
			result.Error = err
			return result
		}
		s.log.Info("Simplified file",
			slog.String("path", path),
			slog.Int("rules_applied", len(result.RulesApplied)),
		)
	}

	return result
}

// SimplifyModifiedFiles simplifies all files in git diff.
func (s *CodeSimplifier) SimplifyModifiedFiles(ctx context.Context, projectPath string) ([]*SimplificationResult, error) {
	if !s.config.Enabled {
		return nil, nil
	}

	git := NewGitOperations(projectPath)

	// Get modified files
	files, err := git.GetChangedFiles(ctx)
	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		s.log.Debug("No modified files to simplify")
		return nil, nil
	}

	var results []*SimplificationResult

	for _, file := range files {
		// Skip non-code files
		if !isCodeFile(file) {
			continue
		}

		fullPath := filepath.Join(projectPath, file)

		// Check if file exists (might have been deleted)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			continue
		}

		result := s.SimplifyFile(fullPath)
		results = append(results, result)
	}

	// Log summary
	changed := 0
	for _, r := range results {
		if r.Changed {
			changed++
		}
	}

	s.log.Info("Simplification complete",
		slog.Int("files_checked", len(results)),
		slog.Int("files_changed", changed),
	)

	return results, nil
}

// SimplifyModifiedFilesInProject is a convenience function for simplifying modified files.
func SimplifyModifiedFilesInProject(ctx context.Context, projectPath string, config *SimplifyConfig) error {
	simplifier := NewCodeSimplifier(config, nil)
	_, err := simplifier.SimplifyModifiedFiles(ctx, projectPath)
	return err
}

// detectLanguage returns language identifier from file extension.
func detectLanguage(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go":
		return "go"
	case ".js", ".jsx", ".mjs":
		return "js"
	case ".ts", ".tsx":
		return "ts"
	case ".py":
		return "py"
	case ".java":
		return "java"
	case ".rs":
		return "rust"
	case ".rb":
		return "ruby"
	case ".php":
		return "php"
	case ".cs":
		return "csharp"
	case ".cpp", ".cc", ".cxx", ".c", ".h", ".hpp":
		return "cpp"
	default:
		return ""
	}
}

// containsLanguage checks if the language is in the list.
func containsLanguage(languages []string, lang string) bool {
	for _, l := range languages {
		if l == lang {
			return true
		}
	}
	return false
}

// isCodeFile returns true if the file is a code file.
func isCodeFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	codeExtensions := map[string]bool{
		".go":   true,
		".js":   true,
		".jsx":  true,
		".ts":   true,
		".tsx":  true,
		".py":   true,
		".java": true,
		".rs":   true,
		".rb":   true,
		".php":  true,
		".cs":   true,
		".cpp":  true,
		".cc":   true,
		".c":    true,
		".h":    true,
		".hpp":  true,
		".mjs":  true,
	}
	return codeExtensions[ext]
}
