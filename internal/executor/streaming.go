package executor

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
)

// OnProgress registers a callback function to receive progress updates during task execution.
// The callback is invoked whenever the execution phase changes or significant events occur.
// Deprecated: Use AddProgressCallback for multi-listener support. This method remains for
// backward compatibility but will overwrite any callback set via OnProgress (not AddProgressCallback).
func (r *Runner) OnProgress(callback ProgressCallback) {
	r.onProgress = callback
}

// AddProgressCallback registers a named callback for progress updates.
// Multiple callbacks can be registered with different names. Use RemoveProgressCallback
// to unregister. This is thread-safe and works alongside the legacy OnProgress callback.
func (r *Runner) AddProgressCallback(name string, callback ProgressCallback) {
	r.progressMu.Lock()
	defer r.progressMu.Unlock()
	if r.progressCallbacks == nil {
		r.progressCallbacks = make(map[string]ProgressCallback)
	}
	r.progressCallbacks[name] = callback
}

// RemoveProgressCallback removes a named callback registered via AddProgressCallback.
func (r *Runner) RemoveProgressCallback(name string) {
	r.progressMu.Lock()
	defer r.progressMu.Unlock()
	delete(r.progressCallbacks, name)
}

// reportProgress sends a progress update to all registered callbacks.
// Progress is monotonic — values lower than the current high-water mark
// for a task are clamped upward to prevent dashboard progress regression.
func (r *Runner) reportProgress(taskID, phase string, progress int, message string) {
	// Enforce monotonic progress per task (never go backwards)
	if progress < 100 { // Allow 100 from any state (completion/failure)
		r.taskProgressMu.Lock()
		if r.taskProgress == nil {
			r.taskProgress = make(map[string]int)
		}
		if prev, ok := r.taskProgress[taskID]; ok && progress < prev {
			progress = prev // Clamp to high-water mark
		}
		r.taskProgress[taskID] = progress
		r.taskProgressMu.Unlock()
	} else {
		// Task done — clean up tracking
		r.taskProgressMu.Lock()
		delete(r.taskProgress, taskID)
		r.taskProgressMu.Unlock()
	}

	// Log progress unless suppressed (e.g., when visual progress display is active)
	if !r.suppressProgressLogs {
		r.log.Info("Task progress",
			slog.String("task_id", taskID),
			slog.String("phase", phase),
			slog.Int("progress", progress),
			slog.String("message", message),
		)
	}

	// Send to legacy callback (e.g., Telegram) if registered
	if r.onProgress != nil {
		r.onProgress(taskID, phase, progress, message)
	}

	// Send to all named callbacks (e.g., dashboard, monitors)
	r.progressMu.RLock()
	callbacks := make([]ProgressCallback, 0, len(r.progressCallbacks))
	for _, cb := range r.progressCallbacks {
		callbacks = append(callbacks, cb)
	}
	r.progressMu.RUnlock()

	for _, cb := range callbacks {
		cb(taskID, phase, progress, message)
	}
}

// parseStreamEvent converts Claude Code stream-json to BackendEvent.
// This function was moved from ClaudeCodeBackend to support stream parsing
// across different backend implementations.
func parseStreamEvent(line string) BackendEvent {
	event := BackendEvent{
		Raw: line,
	}

	var streamEvent StreamEvent
	if err := json.Unmarshal([]byte(line), &streamEvent); err != nil {
		// Not valid JSON, return as-is
		event.Type = EventTypeText
		event.Message = line
		return event
	}

	// Map stream event type to backend event type
	switch streamEvent.Type {
	case "system":
		if streamEvent.Subtype == "init" {
			event.Type = EventTypeInit
			event.Message = "Claude Code initialized"
		}

	case "assistant":
		if streamEvent.Message != nil {
			for _, block := range streamEvent.Message.Content {
				switch block.Type {
				case "tool_use":
					event.Type = EventTypeToolUse
					event.ToolName = block.Name
					event.ToolInput = block.Input
					event.Message = fmt.Sprintf("Using %s", block.Name)
				case "text":
					event.Type = EventTypeText
					event.Message = block.Text
				}
			}
		}

	case "user":
		// Tool results
		if streamEvent.ToolUseResult != nil {
			event.Type = EventTypeToolResult
			var toolResult ToolResultContent
			if err := json.Unmarshal(streamEvent.ToolUseResult, &toolResult); err == nil {
				event.ToolResult = toolResult.Content
				event.IsError = toolResult.IsError
			}
		}

	case "result":
		event.Type = EventTypeResult
		event.Message = streamEvent.Result
		event.IsError = streamEvent.IsError
	}

	// Capture usage info
	if streamEvent.Usage != nil {
		event.TokensInput = streamEvent.Usage.InputTokens
		event.TokensOutput = streamEvent.Usage.OutputTokens
	}
	if streamEvent.Model != "" {
		event.Model = streamEvent.Model
	}

	return event
}

// processBackendEvent handles events from any backend and updates progress state.
// This is the unified event handler that works with both Claude Code and OpenCode.
func (r *Runner) processBackendEvent(taskID string, event BackendEvent, state *progressState) {
	// Track token usage
	state.tokensInput += event.TokensInput
	state.tokensOutput += event.TokensOutput

	// Track model
	if event.Model != "" {
		state.modelName = event.Model
	}

	// Extract commit SHA from various event types - this will be handled in tool results

	// Handle different event types
	switch event.Type {
	case EventTypeInit:
		r.reportProgress(taskID, "🚀 Started", 5, event.Message)

	case EventTypeText:
		// Parse Navigator-specific patterns from text
		if strings.TrimSpace(event.Message) != "" {
			r.parseNavigatorPatterns(taskID, event.Message, state)
		}

	case EventTypeToolUse:
		r.handleToolUse(taskID, event.ToolName, event.ToolInput, state)

	case EventTypeToolResult:
		// Extract commit SHA from tool output
		if event.ToolResult != "" {
			extractCommitSHA(event.ToolResult, state)
		}

	case EventTypeResult:
		// Final result - no specific progress action needed
		// Token usage already tracked above

		// Note: EventTypeNavPhase was removed as it's not used in the current codebase
	}
}

// parseNavigatorPatterns detects Navigator-specific progress signals from text
func (r *Runner) parseNavigatorPatterns(taskID, text string, state *progressState) {
	// Try structured signal parser v2 first (GH-960)
	if r.signalParser != nil {
		signals := r.signalParser.ParseSignals(text)
		if len(signals) > 0 {
			r.handleStructuredSignals(taskID, signals, state)
			return
		}
	}

	// Navigator session start detection
	if strings.Contains(text, "Navigator session started") || strings.Contains(text, "🧭 Navigator") {
		r.reportProgress(taskID, "Navigator", 10, "Navigator session started")
		return
	}

	// Status block patterns (v1 legacy support)
	if strings.Contains(text, "┌────────────────────────────────────────┐") {
		r.parseNavigatorStatusBlock(taskID, text, state)
		return
	}

	// Phase indicators (both old and new patterns)
	phasePatterns := map[string][]string{
		"Research":   {"RESEARCH", "Research", "research"},
		"Implement":  {"IMPL", "IMPLEMENTATION", "Implement", "implement"},
		"Verify":     {"VERIFY", "Verify", "verify"},
		"Init":       {"INIT", "Init", "init"},
		"Complete":   {"COMPLETE", "Complete", "complete"},
	}

	for phase, patterns := range phasePatterns {
		for _, pattern := range patterns {
			if strings.Contains(text, fmt.Sprintf("Phase: %s", pattern)) || strings.Contains(text, fmt.Sprintf("phase\": \"%s\"", strings.ToLower(pattern))) {
				r.handleNavigatorPhase(taskID, phase, state)
			}
		}
		return
	}

	// Workflow check pattern
	if strings.Contains(text, "WORKFLOW CHECK") {
		r.reportProgress(taskID, "Analyzing", 12, "Workflow check...")
		return
	}

	// Task mode detection
	if strings.Contains(text, "Task mode") || strings.Contains(text, "Mode: TASK") {
		r.reportProgress(taskID, "Task Mode", 15, "Task mode activated")
		return
	}

	// Completion signals
	if strings.Contains(text, "EXIT_SIGNAL") || strings.Contains(text, "Task complete") {
		r.reportProgress(taskID, "Completing", 95, "Task complete signal received")
		return
	}

	// Exit signal detection
	if strings.Contains(text, "exit_signal") || strings.Contains(text, "exitSignal") {
		r.reportProgress(taskID, "Finishing", 92, "Exit signal detected")
		return
	}

	// Stagnation detection
	if strings.Contains(text, "stagnation detected") || strings.Contains(text, "Navigator detected stagnation") {
		r.reportProgress(taskID, "⚠️ Stalled", 0, "Navigator detected stagnation")
		return
	}
}

// parseNavigatorStatusBlock extracts progress from Navigator status block
func (r *Runner) parseNavigatorStatusBlock(taskID, text string, state *progressState) {
	// Extract Phase: from status block
	if idx := strings.Index(text, "Phase:"); idx != -1 {
		line := text[idx:]
		// Find end of line
		if end := strings.Index(line, "\n"); end != -1 {
			line = line[:end]
		}
		// Extract phase name
		parts := strings.Split(line, ":")
		if len(parts) > 1 {
			phase := strings.TrimSpace(parts[1])
			r.handleNavigatorPhase(taskID, phase, state)
		}
	}

	// Extract Progress: from status block
	if idx := strings.Index(text, "Progress:"); idx != -1 {
		line := text[idx:]
		// Find end of line
		if end := strings.Index(line, "\n"); end != -1 {
			line = line[:end]
		}
		// Extract progress number
		re := regexp.MustCompile(`Progress:\s*(\d+)%?`)
		if matches := re.FindStringSubmatch(line); len(matches) > 1 {
			if progress, err := strconv.Atoi(matches[1]); err == nil {
				r.reportProgress(taskID, state.phase, progress, "Status update")
			}
		}
	}

	// Extract Task: for context (note: task context is tracked elsewhere)
}

// handleStructuredSignals processes v2 structured pilot signals (GH-960)
func (r *Runner) handleStructuredSignals(taskID string, signals []PilotSignal, state *progressState) {
	if len(signals) == 0 {
		return
	}

	for _, signal := range signals {
		// Determine message
		message := signal.Message
		if message == "" {
			message = fmt.Sprintf("%s signal", signal.Type)
		}

		switch signal.Type {
		case SignalTypeStatus:
			// Update phase if provided
			if signal.Phase != "" {
				r.handleNavigatorPhase(taskID, signal.Phase, state)
			}
			// Update progress if provided
			if signal.Progress > 0 {
				r.reportProgress(taskID, signal.Phase, signal.Progress, message)
			}

		case SignalTypePhase:
			if signal.Phase != "" {
				r.handleNavigatorPhase(taskID, signal.Phase, state)
			}

		case SignalTypeExit:
			state.exitSignal = true
			r.reportProgress(taskID, "Finishing", 95, signal.Message)

		case SignalTypeStagnation:
			r.reportProgress(taskID, "⚠️ Stalled", 0, "Navigator detected stagnation")
		}

		// Check for exit signal from any signal type
		if signal.ExitSignal {
			state.exitSignal = true
			message := "Exit signal received"
			if signal.Success {
				message = "Task completed successfully"
			}
			r.reportProgress(taskID, "Finishing", 92, message)
		}
	}
}

// handleNavigatorPhase maps Navigator phases to progress
func (r *Runner) handleNavigatorPhase(taskID, phase string, state *progressState) {
	phase = strings.ToUpper(strings.TrimSpace(phase))

	// Skip if same phase
	if state.navPhase == phase {
		return
	}

	state.navPhase = phase

	// Map phases to progress ranges and display names
	var displayPhase string
	var progress int
	var message string

	switch phase {
	case "INIT":
		displayPhase = "Init"
		progress = 10
		message = "Initializing task..."
	case "RESEARCH":
		displayPhase = "Research"
		progress = 25
		message = "Analyzing codebase..."
	case "IMPL", "IMPLEMENTATION":
		displayPhase = "Implement"
		progress = 50
		message = "Writing code..."
	case "VERIFY":
		displayPhase = "Verify"
		progress = 75
		message = "Testing changes..."
	case "COMPLETE":
		displayPhase = "Complete"
		progress = 90
		message = "Finalizing..."
	default:
		// Unknown phase, use as-is with moderate progress
		displayPhase = strings.Title(strings.ToLower(phase))
		progress = 35
		message = fmt.Sprintf("Phase: %s", displayPhase)
	}

	r.reportProgress(taskID, displayPhase, progress, message)
}

// handleToolUse processes tool usage and updates phase-based progress
func (r *Runner) handleToolUse(taskID, toolName string, input map[string]interface{}, state *progressState) {
	// Log tool usage at debug level
	r.log.Debug("Tool used",
		slog.String("task_id", taskID),
		slog.String("tool", toolName),
	)

	// Don't update progress for every tool use to avoid spam
	// Only update for significant tools or phase transitions

	// Determine if this tool indicates a phase change
	var newPhase string
	var progress int
	var message string

	switch toolName {
	case "Read", "Glob", "Grep":
		// Research phase
		if state.phase != "Research" {
			newPhase = "Research"
			progress = 15
			message = "Reading codebase..."
		}

	case "Write", "Edit":
		// Implementation phase
		if state.phase != "Implement" {
			newPhase = "Implement"
			progress = 40
			message = "Writing code..."
		}

	case "Bash":
		// Check if it's a test command
		if bashCommand, ok := input["command"].(string); ok {
			if strings.Contains(bashCommand, "test") ||
				strings.Contains(bashCommand, "build") ||
				strings.Contains(bashCommand, "lint") {
				if state.phase != "Verify" {
					newPhase = "Verify"
					progress = 70
					message = "Running tests..."
				}
			} else if strings.Contains(bashCommand, "git commit") ||
				strings.Contains(bashCommand, "git push") {
				newPhase = "Finalizing"
				progress = 85
				message = "Committing changes..."
			}
		}

	case "WebSearch", "WebFetch":
		// Research phase (web research)
		if state.phase != "Research" {
			newPhase = "Research"
			progress = 20
			message = "Researching online..."
		}
	}

	// Only report progress if phase changed
	if newPhase != "" && newPhase != state.phase {
		state.phase = newPhase
		r.reportProgress(taskID, newPhase, progress, message)
	}
}