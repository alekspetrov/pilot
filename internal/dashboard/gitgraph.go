package dashboard

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"os/exec"
)

// GitGraphMode represents the 3 toggle states.
type GitGraphMode int

const (
	GitGraphHidden GitGraphMode = iota
	GitGraphFull
	GitGraphSmall
)

// gitGraphRefreshInterval is how often the git graph auto-refreshes when visible.
const gitGraphRefreshInterval = 15 * time.Second

// minTerminalWidthForGraph is the minimum terminal width to show the graph panel.
const minTerminalWidthForGraph = 100

// branchColors is the palette cycled for branch line coloring.
var branchColors = []string{
	"#7eb8da", // steel blue
	"#7ec699", // sage green
	"#d4a054", // amber
	"#d48a8a", // dusty rose
	"#8b949e", // mid gray
}

// gitGraph-specific styles.
var (
	gitGraphBorderStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#3d4450")) // slate — matches other panels

	gitGraphFocusBorderStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("#7eb8da")) // steel blue when focused

	gitGraphTitleStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#c9d1d9")) // lightgray

	gitGraphMsgStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#c9d1d9")) // lightgray

	gitGraphAuthorStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#8b949e")) // midgray

	gitGraphSHAStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#6e7681")) // gray dim

	gitGraphDateStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#6e7681")) // gray dim

	gitGraphHeadStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#7eb8da")) // steel blue bold

	gitGraphTagStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#d4a054")) // amber bold

	gitGraphRemoteStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#7ec699")) // sage green

	gitGraphScrollStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#6e7681")) // gray dim

	gitGraphErrorStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#d48a8a")) // dusty rose
)

// GitGraphState holds the parsed git graph data.
type GitGraphState struct {
	Lines       []GitGraphLine
	TotalCount  int
	Error       string
	LastRefresh time.Time
}

// GitGraphLine represents one line of git log --graph output.
type GitGraphLine struct {
	Graph      string // Graph drawing characters (│ * ├ etc.)
	Decoration string // Branch/tag labels (raw, for parsing)
	Message    string // Commit message
	Author     string // Author name
	SHA        string // Short SHA (7 chars)
	Date       string // YYYY-MM-DD
	IsCommit   bool   // True if line has a commit (not just a branch connector)
}

// gitRefreshMsg is sent when git graph data has been refreshed.
type gitRefreshMsg struct {
	state *GitGraphState
	err   error
}

// fetchGitGraph runs git fetch --prune then git log --graph and returns parsed state.
func fetchGitGraph(projectPath string, limit int) (*GitGraphState, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Auto-prune stale remote tracking refs (best-effort)
	pruneCmd := exec.CommandContext(ctx, "git", "-C", projectPath, "fetch", "--prune")
	_ = pruneCmd.Run()

	// Separator used to split custom format fields — unlikely to appear in commit messages.
	const sep = "\x00"

	// Format: %h<sep>%D<sep>%s<sep>%an<sep>%as
	// %h = short SHA, %D = decorations (ref names), %s = subject, %an = author name, %as = author date YYYY-MM-DD
	format := fmt.Sprintf("%%h%s%%D%s%%s%s%%an%s%%as", sep, sep, sep, sep)

	cmd := exec.CommandContext(ctx, "git", "-C", projectPath,
		"log", "--graph", "--all",
		"--decorate=full",
		fmt.Sprintf("--format=%s", format),
		fmt.Sprintf("-n%d", limit),
	)
	output, err := cmd.Output()
	if err != nil {
		return &GitGraphState{Error: err.Error(), LastRefresh: time.Now()}, nil
	}

	lines := parseGitGraphOutput(string(output), sep)
	return &GitGraphState{
		Lines:       lines,
		TotalCount:  len(lines),
		LastRefresh: time.Now(),
	}, nil
}

// parseGitGraphOutput splits raw git log --graph output into structured GitGraphLine values.
// The git --graph output interleaves connector lines (no commit data) with commit lines
// (containing our custom format fields).
func parseGitGraphOutput(raw, sep string) []GitGraphLine {
	if raw == "" {
		return nil
	}

	var lines []GitGraphLine
	rawLines := strings.Split(strings.TrimRight(raw, "\n"), "\n")

	for _, raw := range rawLines {
		line := parseGitGraphLine(raw, sep)
		lines = append(lines, line)
	}

	return lines
}

// parseGitGraphLine parses a single line from git log --graph output.
// Lines without a commit contain only graph characters.
// Commit lines contain graph chars followed by our format fields.
func parseGitGraphLine(raw, sep string) GitGraphLine {
	// Find where the format data starts (after graph chars).
	// The graph part ends when we hit a SHA hex digit sequence.
	// With our format, the first field after graph chars is the SHA (%h).
	// However, the line may also be a pure graph connector (│, /, \, etc.)
	// which won't contain our separator.

	sepIdx := strings.Index(raw, sep)
	if sepIdx < 0 {
		// Pure graph connector line (no commit data)
		return GitGraphLine{
			Graph:    stripTrailingSpaces(raw),
			IsCommit: false,
		}
	}

	// Split: graph prefix is everything before the first field.
	// Find the last non-separator, non-graph character before sepIdx.
	// The SHA starts right after graph chars + optional space.
	// Walk backward from sepIdx to find where non-graph chars start.
	graphPart := ""
	dataPart := raw

	// The graph chars appear at the start; the format data follows.
	// Find the boundary: last graph char before the first separator.
	// Strategy: everything before the first \x00 that isn't a hex digit or space is graph.
	firstFieldEnd := sepIdx
	// Walk backward to find where the SHA (7 hex chars) begins
	shaStart := firstFieldEnd
	for shaStart > 0 && isHexChar(raw[shaStart-1]) {
		shaStart--
	}
	// Graph part is everything before the SHA (may include trailing space)
	graphPart = stripTrailingSpaces(raw[:shaStart])
	dataPart = raw[shaStart:]

	// Split data fields by separator
	fields := strings.SplitN(dataPart, sep, 5)
	if len(fields) < 5 {
		// Malformed — treat as graph line
		return GitGraphLine{Graph: stripTrailingSpaces(raw), IsCommit: false}
	}

	sha := strings.TrimSpace(fields[0])
	decoration := strings.TrimSpace(fields[1])
	message := strings.TrimSpace(fields[2])
	author := strings.TrimSpace(fields[3])
	date := strings.TrimSpace(fields[4])

	return GitGraphLine{
		Graph:      graphPart,
		Decoration: decoration,
		Message:    message,
		Author:     author,
		SHA:        sha,
		Date:       date,
		IsCommit:   sha != "",
	}
}

// isHexChar returns true for hexadecimal digit characters.
func isHexChar(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// stripTrailingSpaces removes trailing whitespace from a string.
func stripTrailingSpaces(s string) string {
	return strings.TrimRight(s, " \t")
}

// refreshGitGraph returns a tea.Cmd that fetches git graph data in the background.
func (m Model) refreshGitGraph() tea.Cmd {
	path := m.projectPath
	if path == "" {
		path = "."
	}
	return func() tea.Msg {
		state, err := fetchGitGraph(path, 200)
		return gitRefreshMsg{state: state, err: err}
	}
}

// gitRefreshTickCmd returns a tick command that fires after gitGraphRefreshInterval.
func gitRefreshTickCmd() tea.Cmd {
	return tea.Tick(gitGraphRefreshInterval, func(t time.Time) tea.Msg {
		return gitRefreshTickMsg(t)
	})
}

// gitRefreshTickMsg is the periodic tick for auto-refresh.
type gitRefreshTickMsg time.Time

// renderGitGraph renders the git graph panel.
// Returns empty string when hidden or terminal too narrow.
func (m Model) renderGitGraph() string {
	if m.gitGraphMode == GitGraphHidden {
		return ""
	}

	// Calculate available width for the graph panel
	// Total terminal width - dashboard panel width - 1 gap char
	graphWidth := m.width - panelTotalWidth - 1
	if graphWidth < 30 || m.width < minTerminalWidthForGraph {
		return ""
	}

	borderSt := gitGraphBorderStyle
	if m.gitGraphFocus {
		borderSt = gitGraphFocusBorderStyle
	}

	// Build content
	var content strings.Builder
	innerWidth := graphWidth - 4 // subtract borders (╭╮) + 2 spaces padding

	if m.gitGraphState == nil {
		content.WriteString("  Loading...")
	} else if m.gitGraphState.Error != "" {
		content.WriteString("  " + gitGraphErrorStyle.Render(truncateVisual(m.gitGraphState.Error, innerWidth-2)))
	} else if len(m.gitGraphState.Lines) == 0 {
		content.WriteString("  No commits found")
	} else {
		// Determine visible lines based on scroll offset
		visibleLines := m.getVisibleGraphLines(graphWidth - 4)
		for i, line := range visibleLines {
			if i > 0 {
				content.WriteString("\n")
			}
			switch m.gitGraphMode {
			case GitGraphFull:
				content.WriteString(m.renderGraphLineFull(line, innerWidth))
			case GitGraphSmall:
				content.WriteString(m.renderGraphLineSmall(line, innerWidth))
			}
		}
	}

	// Scroll indicator
	var scrollIndicator string
	if m.gitGraphState != nil && m.gitGraphState.TotalCount > 0 {
		end := m.gitGraphScroll + m.visibleGraphLineCount(graphWidth-4)
		if end > m.gitGraphState.TotalCount {
			end = m.gitGraphState.TotalCount
		}
		scrollIndicator = gitGraphScrollStyle.Render(
			fmt.Sprintf("[%d-%d of %d]", m.gitGraphScroll+1, end, m.gitGraphState.TotalCount),
		)
	}

	return renderGraphPanel("GIT GRAPH", content.String(), scrollIndicator, graphWidth, borderSt)
}

// getVisibleGraphLines returns the slice of lines visible in the current scroll window.
func (m Model) getVisibleGraphLines(innerWidth int) []GitGraphLine {
	if m.gitGraphState == nil || len(m.gitGraphState.Lines) == 0 {
		return nil
	}
	count := m.visibleGraphLineCount(innerWidth)
	start := m.gitGraphScroll
	if start >= len(m.gitGraphState.Lines) {
		start = len(m.gitGraphState.Lines) - 1
	}
	if start < 0 {
		start = 0
	}
	end := start + count
	if end > len(m.gitGraphState.Lines) {
		end = len(m.gitGraphState.Lines)
	}
	return m.gitGraphState.Lines[start:end]
}

// visibleGraphLineCount returns the number of lines that fit in the graph panel body.
// The panel body height is the terminal height minus the border/header overhead.
// We use a fixed estimate since we don't have exact panel heights.
func (m Model) visibleGraphLineCount(_ int) int {
	// Terminal height minus estimated fixed UI overhead
	// Logo (~4) + update panel (~4, conditional) + metrics cards (10) + queue (variable, ~8)
	// + autopilot (~6) + history (~8) + logs (~12) + help(1) + spacing(~5) = ~54 typical
	// We want graph to fill the remaining height beside the dashboard.
	// Use terminal height - 4 (panel borders + title + scroll indicator).
	count := m.height - 4
	if count < 5 {
		count = 5
	}
	if count > 200 {
		count = 200
	}
	return count
}

// renderGraphLineFull renders one git graph line in full mode:
// graph chars + decorated message + author + SHA + date
func (m Model) renderGraphLineFull(line GitGraphLine, innerWidth int) string {
	if !line.IsCommit {
		// Pure graph connector line — just colorize the graph chars
		return padOrTruncate(colorizeGraphChars(line.Graph), innerWidth)
	}

	// Column widths for full mode:
	// SHA: 7, Date: 10, Author: 15, rest: message + graph + decoration
	shaWidth := 7
	dateWidth := 10
	authorWidth := 15
	// Separator spaces: 1 SHA + 1 date + 1 author = 3
	fixedRight := shaWidth + 1 + dateWidth + 1 + authorWidth + 1
	leftWidth := innerWidth - fixedRight
	if leftWidth < 10 {
		leftWidth = 10
	}

	// Left side: graph + decoration + message
	leftRaw := formatGraphLeft(line, leftWidth)

	// Right columns
	sha := gitGraphSHAStyle.Render(fmt.Sprintf("%-7s", truncateASCII(line.SHA, 7)))
	date := gitGraphDateStyle.Render(fmt.Sprintf("%-10s", truncateASCII(line.Date, 10)))
	author := gitGraphAuthorStyle.Render(padOrTruncate(line.Author, authorWidth))

	return padOrTruncate(leftRaw, leftWidth) + " " + sha + " " + date + " " + author
}

// renderGraphLineSmall renders one git graph line in small mode:
// graph chars + truncated message only (no author/SHA/date)
func (m Model) renderGraphLineSmall(line GitGraphLine, innerWidth int) string {
	if !line.IsCommit {
		return padOrTruncate(colorizeGraphChars(line.Graph), innerWidth)
	}
	return padOrTruncate(formatGraphLeft(line, innerWidth), innerWidth)
}

// formatGraphLeft builds the left portion: colored graph chars + decoration labels + message.
func formatGraphLeft(line GitGraphLine, width int) string {
	graphColored := colorizeGraphChars(line.Graph)
	graphVisualWidth := lipgloss.Width(graphColored)

	// Remaining width for decoration + message
	remaining := width - graphVisualWidth
	if remaining < 5 {
		return padOrTruncate(graphColored, width)
	}

	// Build decoration string
	dec := formatDecoration(line.Decoration)
	decVisualWidth := lipgloss.Width(dec)

	if dec != "" {
		// decoration + space + message
		msgWidth := remaining - decVisualWidth - 1
		if msgWidth < 0 {
			msgWidth = 0
		}
		msg := gitGraphMsgStyle.Render(truncateVisual(line.Message, msgWidth))
		return graphColored + dec + " " + msg
	}

	// No decoration — message fills remaining
	msg := gitGraphMsgStyle.Render(truncateVisual(line.Message, remaining))
	return graphColored + msg
}

// formatDecoration parses the ref decoration string and applies colors.
// Input example: "HEAD -> refs/heads/main, refs/remotes/origin/main, refs/tags/v1.40.0"
func formatDecoration(decoration string) string {
	if decoration == "" {
		return ""
	}

	var parts []string
	refs := strings.Split(decoration, ", ")
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}

		// Shorten ref names for display
		switch {
		case strings.HasPrefix(ref, "HEAD -> refs/heads/"):
			branch := strings.TrimPrefix(ref, "HEAD -> refs/heads/")
			parts = append(parts, gitGraphHeadStyle.Render("HEAD → "+branch))

		case ref == "HEAD":
			parts = append(parts, gitGraphHeadStyle.Render("HEAD"))

		case strings.HasPrefix(ref, "refs/tags/"):
			tag := strings.TrimPrefix(ref, "refs/tags/")
			parts = append(parts, gitGraphTagStyle.Render(tag))

		case strings.HasPrefix(ref, "refs/remotes/"):
			remote := strings.TrimPrefix(ref, "refs/remotes/")
			parts = append(parts, gitGraphRemoteStyle.Render(remote))

		case strings.HasPrefix(ref, "refs/heads/"):
			branch := strings.TrimPrefix(ref, "refs/heads/")
			parts = append(parts, gitGraphHeadStyle.Render(branch))

		default:
			parts = append(parts, gitGraphAuthorStyle.Render(ref))
		}
	}

	if len(parts) == 0 {
		return ""
	}

	joined := strings.Join(parts, gitGraphAuthorStyle.Render(", "))
	return gitGraphAuthorStyle.Render("(") + joined + gitGraphAuthorStyle.Render(")")
}

// colorizeGraphChars applies branch colors to git graph drawing characters.
// It cycles through branchColors based on column position of each character.
func colorizeGraphChars(s string) string {
	if s == "" {
		return ""
	}

	var b strings.Builder
	colIdx := 0
	runes := []rune(s)

	for _, r := range runes {
		switch r {
		case '*':
			// Commit node: steel blue
			b.WriteString(lipgloss.NewStyle().
				Foreground(lipgloss.Color("#7eb8da")).Bold(true).
				Render(string(r)))
		case '│', '|':
			// Vertical line: cycle colors by column
			color := branchColors[colIdx%len(branchColors)]
			b.WriteString(lipgloss.NewStyle().
				Foreground(lipgloss.Color(color)).
				Render(string(r)))
		case '─', '-':
			color := branchColors[colIdx%len(branchColors)]
			b.WriteString(lipgloss.NewStyle().
				Foreground(lipgloss.Color(color)).
				Render(string(r)))
		case '╮', '╯', '\\', '/', '└', '┘', '├', '┤':
			color := branchColors[colIdx%len(branchColors)]
			b.WriteString(lipgloss.NewStyle().
				Foreground(lipgloss.Color(color)).
				Render(string(r)))
		default:
			b.WriteRune(r)
		}
		colIdx++
	}
	return b.String()
}

// truncateASCII truncates a string to at most maxLen bytes (safe for ASCII-only SHA/date).
func truncateASCII(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// renderGraphPanel builds a bordered panel with dynamic width for the git graph.
func renderGraphPanel(title, content, scrollIndicator string, totalWidth int, borderSt lipgloss.Style) string {
	innerWidth := totalWidth - 4 // ╭ + space + space + ╮

	var lines []string

	// Top border: ╭─ TITLE ─────╮
	titleUpper := strings.ToUpper(title)
	prefix := "╭─ "
	prefixWidth := utf8.RuneCountInString(prefix+titleUpper) + 1 // +1 for trailing space
	dashCount := totalWidth - prefixWidth - 1                    // -1 for ╮
	if dashCount < 0 {
		dashCount = 0
	}
	topBorder := borderSt.Render(prefix) +
		gitGraphTitleStyle.Render(titleUpper) +
		borderSt.Render(" "+strings.Repeat("─", dashCount)+"╮")
	lines = append(lines, topBorder)

	// Empty line after title
	lines = append(lines, buildGraphEmptyLine(totalWidth, borderSt))

	// Content lines
	for _, line := range strings.Split(content, "\n") {
		lines = append(lines, buildGraphContentLine(line, innerWidth, borderSt))
	}

	// Scroll indicator line (right-aligned)
	if scrollIndicator != "" {
		indicatorWidth := lipgloss.Width(scrollIndicator)
		padding := innerWidth - indicatorWidth
		if padding < 0 {
			padding = 0
		}
		indicatorLine := borderSt.Render("│") + " " +
			strings.Repeat(" ", padding) + scrollIndicator +
			" " + borderSt.Render("│")
		lines = append(lines, indicatorLine)
	}

	// Bottom border: ╰─────╯
	dashCount = totalWidth - 2
	bottomBorder := borderSt.Render("╰" + strings.Repeat("─", dashCount) + "╯")
	lines = append(lines, bottomBorder)

	return strings.Join(lines, "\n")
}

// buildGraphEmptyLine creates an empty bordered line at the given totalWidth.
func buildGraphEmptyLine(totalWidth int, borderSt lipgloss.Style) string {
	spaceCount := totalWidth - 2
	border := borderSt.Render("│")
	return border + strings.Repeat(" ", spaceCount) + border
}

// buildGraphContentLine creates a bordered content line padded to innerWidth.
func buildGraphContentLine(content string, innerWidth int, borderSt lipgloss.Style) string {
	adjusted := padOrTruncate(content, innerWidth)
	border := borderSt.Render("│")
	return border + " " + adjusted + " " + border
}
