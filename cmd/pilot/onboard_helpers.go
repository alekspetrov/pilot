package main

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// panelWidth for onboard screens (matches dashboard)
const onboardPanelWidth = 69

// Color palette (matching dashboard styles)
var (
	onboardBorderStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#3d4450")) // slate

	onboardSuccessStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#7ec699")) // sage green

	onboardLabelStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#c9d1d9")) // light gray

	onboardValueStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#7eb8da")) // steel blue

	onboardDimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8b949e")) // mid gray

	onboardCursorStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#d4a054")) // amber

	onboardDividerStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#3d4450")) // slate
)

// selectOption displays numbered options, returns 1-based index
func selectOption(reader *bufio.Reader, prompt string, options []string) int {
	fmt.Println()
	fmt.Println("  " + prompt)
	fmt.Println()

	for i, opt := range options {
		fmt.Printf("    %s %s\n", onboardValueStyle.Render(fmt.Sprintf("[%d]", i+1)), opt)
	}
	fmt.Println()

	fmt.Print("  " + onboardCursorStyle.Render("▸") + " ")
	line := readLine(reader)

	// Parse selection
	var idx int
	if _, err := fmt.Sscanf(line, "%d", &idx); err != nil || idx < 1 || idx > len(options) {
		return 1 // Default to first option
	}
	return idx
}

// printStageHeader prints panel header with stage counter
// Output: ╭─ TICKET SOURCE ───...───╮
//
//	│                 [2 of 5] │
func printStageHeader(name string, current, total int) {
	// Top border: ╭─ NAME ─────...─────╮
	titleUpper := strings.ToUpper(name)
	prefix := "╭─ "
	prefixWidth := lipgloss.Width(prefix + titleUpper + " ")

	dashCount := onboardPanelWidth - prefixWidth - 1 // -1 for ╮
	if dashCount < 0 {
		dashCount = 0
	}

	fmt.Println(onboardBorderStyle.Render(prefix) +
		onboardLabelStyle.Render(titleUpper) +
		onboardBorderStyle.Render(" "+strings.Repeat("─", dashCount)+"╮"))

	// Stage counter line: │                 [2 of 5] │
	counter := fmt.Sprintf("[%d of %d]", current, total)
	counterWidth := lipgloss.Width(counter)
	paddingWidth := onboardPanelWidth - 4 - counterWidth // -4 for "│ " + " │"
	if paddingWidth < 0 {
		paddingWidth = 0
	}

	border := onboardBorderStyle.Render("│")
	fmt.Println(border + " " + strings.Repeat(" ", paddingWidth) +
		onboardDimStyle.Render(counter) + " " + border)
}

// printStageFooter prints panel footer
// Output: ╰───────────...───────────╯
func printStageFooter() {
	dashCount := onboardPanelWidth - 2
	line := "╰" + strings.Repeat("─", dashCount) + "╯"
	fmt.Println(onboardBorderStyle.Render(line))
}

// printSectionDivider prints a section divider with label
// Output: ── SECTION ──────────────────────
func printSectionDivider(label string) {
	prefix := "── "
	labelUpper := strings.ToUpper(label)
	prefixWidth := lipgloss.Width(prefix + labelUpper + " ")
	dashCount := onboardPanelWidth - prefixWidth
	if dashCount < 3 {
		dashCount = 3
	}

	fmt.Println()
	fmt.Println(onboardDividerStyle.Render(prefix+labelUpper+" ") +
		onboardDividerStyle.Render(strings.Repeat("─", dashCount)))
	fmt.Println()
}
