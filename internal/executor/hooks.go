package executor

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed hookscripts/*
var embeddedHookScripts embed.FS

// HooksConfig configures Claude Code hooks for quality gates during execution.
// Hooks run inline during Claude execution instead of after completion,
// catching issues while context is still available.
//
// Example YAML configuration:
//
//	executor:
//	  hooks:
//	    enabled: true
//	    run_tests_on_stop: true    # Stop hook runs tests (default when enabled)
//	    block_destructive: true    # PreToolUse hook blocks dangerous commands (default when enabled)
//	    lint_on_save: false       # PostToolUse hook runs linter after file changes
type HooksConfig struct {
	// Enabled controls whether Claude Code hooks are active.
	// When false (default), hooks are not installed and execution proceeds normally.
	Enabled bool `yaml:"enabled"`

	// RunTestsOnStop enables the Stop hook that runs build/tests before Claude finishes.
	// When enabled, Claude must fix any build/test failures before completing.
	// Default: true when Enabled is true
	RunTestsOnStop *bool `yaml:"run_tests_on_stop,omitempty"`

	// BlockDestructive enables the PreToolUse hook that blocks dangerous Bash commands.
	// Prevents commands like "rm -rf /", "git push --force", "DROP TABLE", "git reset --hard".
	// Default: true when Enabled is true
	BlockDestructive *bool `yaml:"block_destructive,omitempty"`

	// LintOnSave enables the PostToolUse hook that runs linter after Edit/Write tools.
	// Automatically formats/lints files after changes.
	// Default: false (opt-in feature)
	LintOnSave bool `yaml:"lint_on_save,omitempty"`
}

// DefaultHooksConfig returns default hooks configuration.
func DefaultHooksConfig() *HooksConfig {
	runTestsOnStop := true
	blockDestructive := true
	return &HooksConfig{
		Enabled:          false, // Disabled by default, opt-in feature
		RunTestsOnStop:   &runTestsOnStop,
		BlockDestructive: &blockDestructive,
		LintOnSave:       false,
	}
}

// ClaudeSettings represents the structure of .claude/settings.json
// Uses Claude Code 2.1.44+ regex-string matcher format
type ClaudeSettings struct {
	Hooks map[string][]HookMatcherEntry `json:"hooks,omitempty"`
}

// HookMatcherEntry defines a matcher-based hook entry (Claude Code 2.1.44+)
// Matcher is a regex string (e.g. "Bash") for PreToolUse/PostToolUse.
// For Stop hooks, Matcher must be omitted entirely (use empty string + omitempty).
type HookMatcherEntry struct {
	Matcher string        `json:"matcher,omitempty"`
	Hooks   []HookCommand `json:"hooks"`
}

// HookCommand defines a single hook command
type HookCommand struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// HookDefinition is kept for backward compatibility with old settings format
type HookDefinition struct {
	Command string `json:"command"`
}

// GenerateClaudeSettings builds the .claude/settings.json structure with hook entries
// Uses Claude Code 2.1.42+ matcher-based array format
func GenerateClaudeSettings(config *HooksConfig, scriptDir string) map[string]interface{} {
	if config == nil || !config.Enabled {
		return map[string]interface{}{}
	}

	hooks := make(map[string][]HookMatcherEntry)

	// Stop hook: run tests before Claude finishes.
	// Stop hooks do not support matchers — omitempty on Matcher ensures the field is absent in JSON.
	if config.RunTestsOnStop == nil || *config.RunTestsOnStop {
		hooks["Stop"] = []HookMatcherEntry{
			{
				Matcher: "", // omitempty: field will be absent in JSON for Stop hooks
				Hooks: []HookCommand{
					{
						Type:    "command",
						Command: filepath.Join(scriptDir, "pilot-stop-gate.sh"),
					},
				},
			},
		}
	}

	// PreToolUse hook: block destructive Bash commands.
	// Matcher is a regex string (Claude Code 2.1.44+ format).
	if config.BlockDestructive == nil || *config.BlockDestructive {
		hooks["PreToolUse"] = []HookMatcherEntry{
			{
				Matcher: "Bash",
				Hooks: []HookCommand{
					{
						Type:    "command",
						Command: filepath.Join(scriptDir, "pilot-bash-guard.sh"),
					},
				},
			},
		}
	}

	// PostToolUse hook: lint files after changes (opt-in).
	if config.LintOnSave {
		hooks["PostToolUse"] = []HookMatcherEntry{
			{
				Matcher: "Edit",
				Hooks: []HookCommand{
					{
						Type:    "command",
						Command: filepath.Join(scriptDir, "pilot-lint.sh"),
					},
				},
			},
			{
				Matcher: "Write",
				Hooks: []HookCommand{
					{
						Type:    "command",
						Command: filepath.Join(scriptDir, "pilot-lint.sh"),
					},
				},
			},
		}
	}

	if len(hooks) == 0 {
		return map[string]interface{}{}
	}

	return map[string]interface{}{
		"hooks": hooks,
	}
}

// WriteClaudeSettings writes the .claude/settings.json file
func WriteClaudeSettings(settingsPath string, settings map[string]interface{}) error {
	// Create directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0755); err != nil {
		return fmt.Errorf("failed to create .claude directory: %w", err)
	}

	// Write settings as JSON
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal settings: %w", err)
	}

	if err := os.WriteFile(settingsPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write settings file: %w", err)
	}

	return nil
}

// MergeWithExisting merges Pilot hooks with existing .claude/settings.json
// Returns a restore function to revert changes and any error
// Handles both old format (map[string]HookDefinition) and new format (map[string][]HookMatcherEntry)
// Stale Pilot hooks (pointing to non-existent scripts) are removed before merging.
func MergeWithExisting(settingsPath string, pilotSettings map[string]interface{}) (restoreFunc func() error, err error) {
	var originalData []byte
	var originalExists bool

	// Read existing settings if they exist
	if data, readErr := os.ReadFile(settingsPath); readErr == nil {
		originalData = data
		originalExists = true
	} else if !os.IsNotExist(readErr) {
		return nil, fmt.Errorf("failed to read existing settings: %w", readErr)
	}

	// If no Pilot hooks to add, no-op
	if len(pilotSettings) == 0 {
		return func() error { return nil }, nil
	}

	var merged map[string]interface{}

	if originalExists && len(originalData) > 0 {
		// Parse existing settings
		var existing map[string]interface{}
		if err := json.Unmarshal(originalData, &existing); err != nil {
			return nil, fmt.Errorf("failed to parse existing settings: %w", err)
		}

		// Deep merge hooks section
		merged = make(map[string]interface{})
		for k, v := range existing {
			merged[k] = v
		}

		if pilotHooks, ok := pilotSettings["hooks"]; ok {
			if existingHooks, exists := merged["hooks"]; exists {
				// Check if existing hooks are in old format (object with command) or new format (arrays)
				if existingHooksMap, ok := existingHooks.(map[string]interface{}); ok {
					if isOldHookFormat(existingHooksMap) {
						// Old format detected - don't corrupt it, just add our new format hooks
						// Keep existing as-is and append our hooks
						merged["hooks"] = pilotHooks
					} else {
						// New format - clean stale pilot hooks, then merge arrays by hook event type
						cleanedHooksMap := cleanStalePilotHooks(existingHooksMap)
						mergedHooks := mergeNewFormatHooks(cleanedHooksMap, pilotHooks)
						merged["hooks"] = mergedHooks
					}
				} else {
					// Unknown format, replace with pilot hooks
					merged["hooks"] = pilotHooks
				}
			} else {
				// No existing hooks, add pilot hooks
				merged["hooks"] = pilotHooks
			}
		}
	} else {
		// No existing settings, use pilot settings directly
		merged = pilotSettings
	}

	// Write merged settings
	if err := WriteClaudeSettings(settingsPath, merged); err != nil {
		return nil, fmt.Errorf("failed to write merged settings: %w", err)
	}

	// Return restore function
	restoreFunc = func() error {
		if originalExists {
			// Restore original file
			return os.WriteFile(settingsPath, originalData, 0644)
		} else {
			// Remove file we created
			if err := os.Remove(settingsPath); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("failed to remove settings file: %w", err)
			}
		}
		return nil
	}

	return restoreFunc, nil
}

// cleanStalePilotHooks removes hook entries whose command paths match the Pilot
// hook script pattern (pilot-hooks-*/pilot-*.sh) but whose script files no longer
// exist on disk (e.g. from a previous crashed session). This prevents stale entries
// from accumulating across restarts.
func cleanStalePilotHooks(hooks map[string]interface{}) map[string]interface{} {
	cleaned := make(map[string]interface{})
	for hookType, v := range hooks {
		arr, ok := v.([]interface{})
		if !ok {
			cleaned[hookType] = v
			continue
		}
		var kept []interface{}
		for _, entry := range arr {
			if isPilotStaleHook(entry) {
				continue // drop stale pilot hook
			}
			kept = append(kept, entry)
		}
		if len(kept) > 0 {
			cleaned[hookType] = kept
		}
	}
	return cleaned
}

// isPilotStaleHook returns true if the entry is a Pilot-managed hook whose script
// file no longer exists on disk.
func isPilotStaleHook(entry interface{}) bool {
	entryMap, ok := entry.(map[string]interface{})
	if !ok {
		return false
	}
	hooksVal, ok := entryMap["hooks"]
	if !ok {
		return false
	}
	hooksArr, ok := hooksVal.([]interface{})
	if !ok {
		return false
	}
	for _, h := range hooksArr {
		hMap, ok := h.(map[string]interface{})
		if !ok {
			continue
		}
		cmd, _ := hMap["command"].(string)
		// Pilot scripts live in os.MkdirTemp("", "pilot-hooks-") directories
		// and are named pilot-*.sh.
		if isPilotHookPath(cmd) {
			if _, err := os.Stat(cmd); os.IsNotExist(err) {
				return true
			}
		}
	}
	return false
}

// isPilotHookPath returns true if the path looks like a Pilot-managed hook script.
// Pattern: .../pilot-hooks-<random>/pilot-*.sh
func isPilotHookPath(path string) bool {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	return strings.HasPrefix(filepath.Base(dir), "pilot-hooks-") &&
		strings.HasPrefix(base, "pilot-") &&
		strings.HasSuffix(base, ".sh")
}

// isOldHookFormat checks if the hooks map is in old format (e.g., "Stop": {"command": "..."})
// Old format has string keys with object values containing "command" field
// New format has string keys with array values
func isOldHookFormat(hooks map[string]interface{}) bool {
	for _, v := range hooks {
		// In old format, each value is an object with "command" field
		if obj, ok := v.(map[string]interface{}); ok {
			if _, hasCommand := obj["command"]; hasCommand {
				return true
			}
		}
		// In new format, each value is an array
		if _, isArray := v.([]interface{}); isArray {
			return false
		}
	}
	return false
}

// mergeNewFormatHooks merges two hook maps in the new array-based format.
// Deduplication: before appending a Pilot hook entry, the script filename is
// extracted from the command path and compared against already-present entries.
// Entries with the same script name (e.g. "pilot-bash-guard.sh") are skipped,
// even if the temp directory differs.
func mergeNewFormatHooks(existing map[string]interface{}, pilot interface{}) map[string]interface{} {
	mergedHooks := make(map[string]interface{})

	// Copy existing hooks
	for k, v := range existing {
		mergedHooks[k] = v
	}

	// collectScriptNames builds a set of script filenames already present in an
	// []interface{} hook array (unmarshaled JSON).
	collectScriptNames := func(arr []interface{}) map[string]struct{} {
		names := make(map[string]struct{})
		for _, item := range arr {
			m, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			hooksVal, _ := m["hooks"].([]interface{})
			for _, h := range hooksVal {
				hm, ok := h.(map[string]interface{})
				if !ok {
					continue
				}
				cmd, _ := hm["command"].(string)
				if cmd != "" {
					names[filepath.Base(cmd)] = struct{}{}
				}
			}
		}
		return names
	}

	// Merge pilot hooks
	switch ph := pilot.(type) {
	case map[string][]HookMatcherEntry:
		for k, v := range ph {
			if existingArr, ok := mergedHooks[k].([]interface{}); ok {
				existing := collectScriptNames(existingArr)
				for _, entry := range v {
					// Collect script names in this pilot entry
					isDup := false
					for _, hc := range entry.Hooks {
						if _, seen := existing[filepath.Base(hc.Command)]; seen {
							isDup = true
							break
						}
					}
					if !isDup {
						existingArr = append(existingArr, entry)
					}
				}
				mergedHooks[k] = existingArr
			} else {
				// No existing entries for this hook type, use pilot's
				mergedHooks[k] = v
			}
		}
	case map[string]interface{}:
		for k, v := range ph {
			if existingArr, ok := mergedHooks[k].([]interface{}); ok {
				existing := collectScriptNames(existingArr)
				if newArr, ok := v.([]interface{}); ok {
					for _, item := range newArr {
						m, ok := item.(map[string]interface{})
						if !ok {
							existingArr = append(existingArr, item)
							continue
						}
						hooksVal, _ := m["hooks"].([]interface{})
						isDup := false
						for _, h := range hooksVal {
							hm, ok := h.(map[string]interface{})
							if !ok {
								continue
							}
							cmd, _ := hm["command"].(string)
							if _, seen := existing[filepath.Base(cmd)]; seen {
								isDup = true
								break
							}
						}
						if !isDup {
							existingArr = append(existingArr, item)
						}
					}
					mergedHooks[k] = existingArr
				} else if newArr, ok := v.([]HookMatcherEntry); ok {
					for _, entry := range newArr {
						isDup := false
						for _, hc := range entry.Hooks {
							if _, seen := existing[filepath.Base(hc.Command)]; seen {
								isDup = true
								break
							}
						}
						if !isDup {
							existingArr = append(existingArr, entry)
						}
					}
					mergedHooks[k] = existingArr
				}
			} else {
				// No existing entries for this hook type, use pilot's
				mergedHooks[k] = v
			}
		}
	}

	return mergedHooks
}

// WriteEmbeddedScripts extracts embedded hook scripts to the specified directory
func WriteEmbeddedScripts(scriptDir string) error {
	// Create directory if it doesn't exist
	if err := os.MkdirAll(scriptDir, 0755); err != nil {
		return fmt.Errorf("failed to create script directory: %w", err)
	}

	// Read embedded scripts
	entries, err := embeddedHookScripts.ReadDir("hookscripts")
	if err != nil {
		return fmt.Errorf("failed to read embedded scripts: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		// Read script content
		content, err := embeddedHookScripts.ReadFile(filepath.Join("hookscripts", entry.Name()))
		if err != nil {
			return fmt.Errorf("failed to read embedded script %s: %w", entry.Name(), err)
		}

		// Write to target directory with executable permissions
		scriptPath := filepath.Join(scriptDir, entry.Name())
		if err := os.WriteFile(scriptPath, content, 0755); err != nil {
			return fmt.Errorf("failed to write script %s: %w", entry.Name(), err)
		}
	}

	return nil
}

// GetBoolPtrValue returns the value of a bool pointer or the default value if nil
func GetBoolPtrValue(ptr *bool, defaultValue bool) bool {
	if ptr == nil {
		return defaultValue
	}
	return *ptr
}

// GetScriptNames returns the list of required script names for validation
func GetScriptNames(config *HooksConfig) []string {
	if config == nil || !config.Enabled {
		return nil
	}

	var scripts []string

	if GetBoolPtrValue(config.RunTestsOnStop, true) {
		scripts = append(scripts, "pilot-stop-gate.sh")
	}

	if GetBoolPtrValue(config.BlockDestructive, true) {
		scripts = append(scripts, "pilot-bash-guard.sh")
	}

	if config.LintOnSave {
		scripts = append(scripts, "pilot-lint.sh")
	}

	return scripts
}