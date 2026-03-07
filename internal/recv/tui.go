package recv

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// SearchMode identifies the type of search filter.
type SearchMode int

const (
	ModeHide SearchMode = iota // /!pattern — remove matching lines
	ModeGrep                   // /=pattern — keep only matching lines
)

// SearchFilter is one entry in the filter stack.
type SearchFilter struct {
	Regex *regexp.Regexp
	Mode  SearchMode
}

// DiskReporter provides disk usage for the TUI.
type DiskReporter interface {
	DiskUsage() int64
}

// TUIModel is the bubbletea model for the live receiver dashboard.
type TUIModel struct {
	stats      *Stats
	ring       *LogRing
	disk       DiskReporter
	diskCap    int64
	writer     *Writer
	listen     string
	dir        string
	redactInfo string

	// snapshots
	prev     Snapshot
	curr     Snapshot
	lastTick time.Time

	// computed rates
	logsPerSec  float64
	bytesPerSec float64

	// log display
	lines       []LogEntry
	scrollOff   int
	follow      bool
	ringVersion int

	// search — filter stack
	searching   bool
	searchInput string
	filterStack []SearchFilter // stacked hide/grep filters
	highlight   *regexp.Regexp // current highlight (not part of stack)
	searchIdx   int            // current match index in highlight results
	matches     []int          // indices into lines for highlight

	// label filter
	filtering    bool
	filterInput  string
	filterKey    string // parsed key
	filterVal    string // parsed value
	filterActive bool

	// export
	exporting   bool
	exportInput string
	exportMsg   string // brief confirmation shown in status bar

	// time jump
	timeJumping   bool
	timeJumpInput string

	// gg detection
	lastGPress time.Time

	// terminal size
	width  int
	height int

	// help overlay
	showHelp bool

	// quit signal
	quitting bool
}

type tickMsg time.Time

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// NewTUIModel creates a TUI model wired to the pipeline data sources.
func NewTUIModel(stats *Stats, ring *LogRing, disk DiskReporter, diskCap int64, writer *Writer, listen, dir, redactInfo string) TUIModel {
	// Force ANSI256 color profile so badge colors render reliably
	// regardless of terminal auto-detection.
	lipgloss.SetColorProfile(termenv.ANSI256)

	return TUIModel{
		stats:      stats,
		ring:       ring,
		disk:       disk,
		diskCap:    diskCap,
		writer:     writer,
		listen:     listen,
		dir:        dir,
		redactInfo: redactInfo,
		follow:     true,
		width:      80,
		height:     24,
	}
}

// Init starts the tick timer.
func (m TUIModel) Init() tea.Cmd {
	return tickCmd()
}

// Update handles messages.
func (m TUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tickMsg:
		now := time.Time(msg)
		m.prev = m.curr

		var diskUsage int64
		if m.disk != nil {
			diskUsage = m.disk.DiskUsage()
		}
		var bytesWritten int64
		if m.writer != nil {
			bytesWritten = m.writer.BytesWritten()
		}
		m.curr = m.stats.Snapshot(diskUsage, m.diskCap, bytesWritten)

		if !m.lastTick.IsZero() {
			elapsed := now.Sub(m.lastTick).Seconds()
			if elapsed > 0 {
				m.logsPerSec = float64(m.curr.LogsReceived-m.prev.LogsReceived) / elapsed
				m.bytesPerSec = float64(m.curr.BytesWritten-m.prev.BytesWritten) / elapsed
			}
		}
		m.lastTick = now

		newVersion := m.ring.Version()
		if newVersion != m.ringVersion {
			if m.filterActive {
				m.lines = m.ring.SnapshotFiltered(m.labelPredicate())
			} else {
				m.lines = m.ring.Snapshot()
			}
			m.ringVersion = newVersion
			m.applySearchFilter()
			m.updateSearchMatches()
			if m.follow {
				m.scrollToBottom()
			}
		}

		return m, tickCmd()

	case tea.KeyMsg:
		if m.showHelp {
			if msg.String() == "?" || msg.String() == "esc" || msg.String() == "q" {
				m.showHelp = false
			}
			return m, nil
		}
		if m.exporting {
			return m.updateExport(msg)
		}
		if m.timeJumping {
			return m.updateTimeJump(msg)
		}
		if m.filtering {
			return m.updateFilter(msg)
		}
		if m.searching {
			return m.updateSearch(msg)
		}
		return m.updateNormal(msg)
	}

	return m, nil
}

func (m TUIModel) updateNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		m.quitting = true
		return m, tea.Quit

	case "j", "down":
		m.follow = false
		m.scrollOff = clamp(m.scrollOff+1, 0, m.maxScroll())

	case "k", "up":
		m.follow = false
		m.scrollOff = clamp(m.scrollOff-1, 0, m.maxScroll())

	case "d":
		m.follow = false
		half := m.logPaneHeight() / 2
		m.scrollOff = clamp(m.scrollOff+half, 0, m.maxScroll())

	case "u":
		m.follow = false
		half := m.logPaneHeight() / 2
		m.scrollOff = clamp(m.scrollOff-half, 0, m.maxScroll())

	case "G":
		m.follow = true
		m.scrollToBottom()

	case "g":
		now := time.Now()
		if now.Sub(m.lastGPress) < 500*time.Millisecond {
			m.follow = false
			m.scrollOff = 0
			m.lastGPress = time.Time{}
		} else {
			m.lastGPress = now
		}

	case "f":
		m.follow = !m.follow
		if m.follow {
			m.scrollToBottom()
		}

	case "esc":
		// unwind: clear highlight first, then pop stack
		if m.highlight != nil {
			m.highlight = nil
			m.matches = nil
		} else if len(m.filterStack) > 0 {
			m.filterStack = m.filterStack[:len(m.filterStack)-1]
			m.ringVersion = -1
		}

	case "/":
		m.searching = true
		m.searchInput = ""

	case "n":
		m.nextMatch(1)

	case "N":
		m.nextMatch(-1)

	case "l":
		m.filtering = true
		m.filterInput = ""

	case "t":
		m.timeJumping = true
		m.timeJumpInput = ""

	case "w":
		m.exporting = true
		m.exportInput = fmt.Sprintf("./filtered-%s", time.Now().Format("20060102-150405"))
		m.exportMsg = ""

	case "?":
		m.showHelp = !m.showHelp
	}

	return m, nil
}

func (m TUIModel) updateExport(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.exporting = false
		n, err := ExportFilteredLines(m.lines, m.exportInput)
		if err != nil {
			m.exportMsg = fmt.Sprintf("Export error: %s", err)
		} else {
			m.exportMsg = fmt.Sprintf("Exported %d lines to %s", n, m.exportInput)
		}

	case "esc":
		m.exporting = false
		m.exportInput = ""

	case "backspace":
		if len(m.exportInput) > 0 {
			m.exportInput = m.exportInput[:len(m.exportInput)-1]
		}

	default:
		if len(msg.String()) == 1 {
			m.exportInput += msg.String()
		}
	}

	return m, nil
}

func (m TUIModel) updateFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.filtering = false
		if m.filterInput == "" {
			m.filterActive = false
			m.filterKey = ""
			m.filterVal = ""
		} else if idx := strings.IndexByte(m.filterInput, '='); idx > 0 {
			m.filterKey = m.filterInput[:idx]
			m.filterVal = m.filterInput[idx+1:]
			m.filterActive = true
		}
		// force re-read from ring
		m.ringVersion = -1

	case "esc":
		m.filtering = false
		m.filterInput = ""
		m.filterActive = false
		m.filterKey = ""
		m.filterVal = ""
		// force re-read from ring
		m.ringVersion = -1

	case "backspace":
		if len(m.filterInput) > 0 {
			m.filterInput = m.filterInput[:len(m.filterInput)-1]
		}

	default:
		if len(msg.String()) == 1 {
			m.filterInput += msg.String()
		}
	}

	return m, nil
}

func (m TUIModel) updateTimeJump(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.timeJumping = false
		idx := FindTimeIndex(m.lines, m.timeJumpInput)
		if idx >= 0 {
			m.follow = false
			m.scrollOff = clamp(idx-m.logPaneHeight()/2, 0, m.maxScroll())
		}

	case "esc":
		m.timeJumping = false
		m.timeJumpInput = ""

	case "backspace":
		if len(m.timeJumpInput) > 0 {
			m.timeJumpInput = m.timeJumpInput[:len(m.timeJumpInput)-1]
		}

	default:
		if len(msg.String()) == 1 {
			m.timeJumpInput += msg.String()
		}
	}

	return m, nil
}

// FindTimeIndex finds the first line whose formatted timestamp contains the input fragment.
// Supports fragments like: "14:32", "14:32:05", "2026-03-05T14:32".
// Returns -1 if no match.
func FindTimeIndex(lines []LogEntry, input string) int {
	input = strings.TrimSpace(input)
	if input == "" || len(lines) == 0 {
		return -1
	}
	for i, entry := range lines {
		ts := entry.Timestamp.Format("2006-01-02T15:04:05Z")
		if strings.Contains(ts, input) {
			return i
		}
	}
	return -1
}

func (m TUIModel) labelPredicate() func(LogEntry) bool {
	key, val := m.filterKey, m.filterVal
	return func(e LogEntry) bool {
		v, ok := e.Labels[key]
		return ok && v == val
	}
}

func (m TUIModel) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.searching = false
		input := m.searchInput
		if strings.HasPrefix(input, "!") {
			// hide mode — push onto stack
			re, err := regexp.Compile(input[1:])
			if err == nil {
				m.filterStack = append(m.filterStack, SearchFilter{Regex: re, Mode: ModeHide})
				m.applySearchFilter()
				m.scrollOff = 0
				if m.follow {
					m.scrollToBottom()
				}
			}
		} else if strings.HasPrefix(input, "=") {
			// grep mode — push onto stack
			re, err := regexp.Compile(input[1:])
			if err == nil {
				m.filterStack = append(m.filterStack, SearchFilter{Regex: re, Mode: ModeGrep})
				m.applySearchFilter()
				m.scrollOff = 0
				if m.follow {
					m.scrollToBottom()
				}
			}
		} else {
			// highlight mode — replaces current highlight, not part of stack
			re, err := regexp.Compile(input)
			if err == nil {
				m.highlight = re
				m.updateSearchMatches()
				m.searchIdx = 0
				if len(m.matches) > 0 {
					m.follow = false
					m.scrollOff = clamp(m.matches[0]-m.logPaneHeight()/2, 0, m.maxScroll())
				}
			}
		}

	case "esc":
		// cancel search input — just close the prompt
		m.searching = false
		m.searchInput = ""

	case "backspace":
		if len(m.searchInput) > 0 {
			m.searchInput = m.searchInput[:len(m.searchInput)-1]
		}

	default:
		if len(msg.String()) == 1 {
			m.searchInput += msg.String()
		}
	}

	return m, nil
}

// applySearchFilter applies the full filter stack to m.lines in order.
func (m *TUIModel) applySearchFilter() {
	for _, f := range m.filterStack {
		switch f.Mode {
		case ModeGrep:
			filtered := make([]LogEntry, 0)
			for _, entry := range m.lines {
				if EntryMatchesRegex(entry, f.Regex) {
					filtered = append(filtered, entry)
				}
			}
			m.lines = filtered
		case ModeHide:
			filtered := make([]LogEntry, 0, len(m.lines))
			for _, entry := range m.lines {
				if !EntryMatchesRegex(entry, f.Regex) {
					filtered = append(filtered, entry)
				}
			}
			m.lines = filtered
		}
	}
}

// EntryMatchesRegex checks whether a log entry matches the given regex.
func EntryMatchesRegex(entry LogEntry, re *regexp.Regexp) bool {
	if re.MatchString(entry.Message) {
		return true
	}
	for _, v := range entry.Labels {
		if re.MatchString(v) {
			return true
		}
	}
	return false
}

func (m *TUIModel) updateSearchMatches() {
	m.matches = nil
	if m.highlight == nil {
		return
	}
	for i, entry := range m.lines {
		if EntryMatchesRegex(entry, m.highlight) {
			m.matches = append(m.matches, i)
		}
	}
}

func (m *TUIModel) nextMatch(dir int) {
	if len(m.matches) == 0 {
		return
	}
	m.searchIdx = (m.searchIdx + dir + len(m.matches)) % len(m.matches)
	target := m.matches[m.searchIdx]
	m.follow = false
	m.scrollOff = clamp(target-m.logPaneHeight()/2, 0, m.maxScroll())
}

func (m *TUIModel) scrollToBottom() {
	m.scrollOff = m.maxScroll()
}

func (m TUIModel) logPaneHeight() int {
	// header(1) + blank(1) + stats(6) + separator(1) = 9 lines overhead
	h := m.height - 9
	if h < 1 {
		h = 1
	}
	return h
}

func (m TUIModel) maxScroll() int {
	max := len(m.lines) - m.logPaneHeight()
	if max < 0 {
		return 0
	}
	return max
}

// View renders the TUI.
func (m TUIModel) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder

	// header
	header := headerStyle.Render(fmt.Sprintf("logtap v0.1.0 | %s | %s", m.listen, m.dir))
	b.WriteString(header)
	b.WriteString("\n\n")

	// stats + talkers side by side
	statsCol := m.renderStats()
	talkersCol := m.renderTalkers()

	leftW := m.width / 2
	if leftW < 30 {
		leftW = 30
	}
	rightW := m.width - leftW
	if rightW < 0 {
		rightW = 0
	}

	statsLines := strings.Split(statsCol, "\n")
	talkerLines := strings.Split(talkersCol, "\n")

	maxLines := len(statsLines)
	if len(talkerLines) > maxLines {
		maxLines = len(talkerLines)
	}

	for i := 0; i < maxLines; i++ {
		left := ""
		if i < len(statsLines) {
			left = statsLines[i]
		}
		right := ""
		if i < len(talkerLines) {
			right = talkerLines[i]
		}
		leftPadded := padRight(left, leftW)
		b.WriteString(leftPadded)
		b.WriteString(right)
		if rightW > 0 {
			b.WriteString("\n")
		} else {
			b.WriteString("\n")
		}
	}

	// separator
	b.WriteString(sepStyle.Render(strings.Repeat("─", m.width)))
	b.WriteString("\n")

	// log pane (or help overlay)
	paneH := m.logPaneHeight()

	if m.showHelp {
		helpLines := m.renderHelp()
		for i := 0; i < paneH; i++ {
			if i < len(helpLines) {
				b.WriteString(helpLines[i])
			}
			b.WriteString("\n")
		}
	} else {
		end := m.scrollOff + paneH
		if end > len(m.lines) {
			end = len(m.lines)
		}
		start := m.scrollOff
		if start < 0 {
			start = 0
		}

		matchSet := make(map[int]bool, len(m.matches))
		for _, idx := range m.matches {
			matchSet[idx] = true
		}

		for i := start; i < end; i++ {
			entry := m.lines[i]
			ts := entry.Timestamp.Format("2006-01-02T15:04:05Z")
			app := entry.Labels["app"]
			if app == "" {
				for _, v := range entry.Labels {
					app = v
					break
				}
			}

			line := fmt.Sprintf("%s [%s] %s", ts, app, entry.Message)
			if len(line) > m.width {
				line = line[:m.width]
			}

			if matchSet[i] {
				b.WriteString(matchStyle.Render(line))
			} else {
				b.WriteString(logLineStyle.Render(line))
			}
			b.WriteString("\n")
		}

		// pad remaining lines
		for i := end - start; i < paneH; i++ {
			b.WriteString("\n")
		}
	}

	// status bar
	var status strings.Builder
	if m.exporting {
		status.WriteString(searchBadge.Render(fmt.Sprintf("w:%s", m.exportInput)))
	} else if m.exportMsg != "" {
		status.WriteString(exportBadge.Render(m.exportMsg))
	}
	if m.timeJumping {
		status.WriteString(searchBadge.Render(fmt.Sprintf("t:%s", m.timeJumpInput)))
	}
	if m.filtering {
		if status.Len() > 0 {
			status.WriteString(" ")
		}
		status.WriteString(filterBadge.Render(fmt.Sprintf("filter: %s", m.filterInput)))
	} else if m.filterActive {
		status.WriteString(filterBadge.Render(fmt.Sprintf("FILTER: %s=%s", m.filterKey, m.filterVal)))
	}
	// filter stack badges
	for _, f := range m.filterStack {
		if status.Len() > 0 {
			status.WriteString(" ")
		}
		switch f.Mode {
		case ModeHide:
			status.WriteString(filterBadge.Render(fmt.Sprintf("HIDE: /%s", f.Regex.String())))
		case ModeGrep:
			status.WriteString(grepBadge.Render(fmt.Sprintf("GREP: /%s", f.Regex.String())))
		}
	}
	// line count after stack
	if len(m.filterStack) > 0 {
		if status.Len() > 0 {
			status.WriteString(" ")
		}
		status.WriteString(filterBadge.Render(fmt.Sprintf("[%d lines]", len(m.lines))))
	}
	if m.searching {
		if status.Len() > 0 {
			status.WriteString(" ")
		}
		status.WriteString(searchBadge.Render(fmt.Sprintf("/%s", m.searchInput)))
	} else if m.highlight != nil {
		if status.Len() > 0 {
			status.WriteString(" ")
		}
		status.WriteString(searchBadge.Render(fmt.Sprintf("[%d/%d] /%s", m.searchIdx+1, len(m.matches), m.highlight.String())))
	}
	if m.follow {
		if status.Len() > 0 {
			status.WriteString(" ")
		}
		status.WriteString(followBadge.Render("FOLLOW"))
	}
	if status.Len() > 0 {
		b.WriteString(padLeft(status.String(), m.width))
	}

	return b.String()
}

func (m TUIModel) renderStats() string {
	var b strings.Builder
	b.WriteString(labelStyle.Render(" Connections:  "))
	b.WriteString(fmt.Sprintf("%d\n", m.curr.ActiveConns))
	b.WriteString(labelStyle.Render(" Logs/sec:     "))
	b.WriteString(fmt.Sprintf("%s\n", formatRate(m.logsPerSec)))
	b.WriteString(labelStyle.Render(" Bytes/sec:    "))
	b.WriteString(fmt.Sprintf("%s\n", formatBytes(int64(m.bytesPerSec))))
	b.WriteString(labelStyle.Render(" Disk used:    "))
	b.WriteString(fmt.Sprintf("%s / %s\n", formatBytes(m.curr.DiskUsage), formatBytes(m.curr.DiskCap)))
	b.WriteString(labelStyle.Render(" Dropped:      "))
	if m.curr.LogsDropped > 0 {
		b.WriteString(droppedStyle.Render(fmt.Sprintf("%d", m.curr.LogsDropped)))
	} else {
		b.WriteString("0")
	}
	b.WriteString("\n")
	b.WriteString(labelStyle.Render(" Redact:        "))
	if m.redactInfo != "" {
		b.WriteString(m.redactInfo)
	} else {
		b.WriteString(warnStyle.Render("OFF — captured logs may contain sensitive data (use --redact)"))
	}
	return b.String()
}

func (m TUIModel) renderTalkers() string {
	var b strings.Builder
	b.WriteString(boldStyle.Render("Top talkers"))
	b.WriteString("\n")
	limit := 5
	if len(m.curr.Talkers) < limit {
		limit = len(m.curr.Talkers)
	}
	for i := 0; i < limit; i++ {
		t := m.curr.Talkers[i]
		var rate float64
		if !m.lastTick.IsZero() {
			// find prev count for this talker
			for _, pt := range m.prev.Talkers {
				if pt.Name == t.Name {
					elapsed := time.Since(m.lastTick).Seconds() + 1 // approximate 1s tick
					rate = float64(t.Count-pt.Count) / elapsed
					break
				}
			}
		}
		if rate > 0 {
			b.WriteString(fmt.Sprintf(" %-20s %s/s\n", t.Name, formatRate(rate)))
		} else {
			b.WriteString(fmt.Sprintf(" %-20s %s total\n", t.Name, formatRate(float64(t.Count))))
		}
	}
	// pad to 5 lines
	for i := limit; i < 5; i++ {
		b.WriteString("\n")
	}
	return b.String()
}

func (m TUIModel) renderHelp() []string {
	h := helpKeyStyle
	d := helpDescStyle
	return []string{
		boldStyle.Render("  Keybindings") + labelStyle.Render("  (press ? or Esc to close)"),
		"",
		h.Render("  Navigation"),
		d.Render("    j/k        ") + "scroll up/down",
		d.Render("    d/u        ") + "half-page down/up",
		d.Render("    G          ") + "jump to bottom (follow)",
		d.Render("    gg         ") + "jump to top",
		d.Render("    f          ") + "toggle follow mode",
		"",
		h.Render("  Search"),
		d.Render("    /pattern   ") + "highlight matches, n/N to navigate",
		d.Render("    /!pattern  ") + "hide matching lines",
		d.Render("    /=pattern  ") + "show only matching lines (grep)",
		d.Render("    Esc        ") + "clear search",
		"",
		h.Render("  Filter"),
		d.Render("    l          ") + "label filter (e.g. container=api)",
		d.Render("    t          ") + "jump to timestamp (e.g. 14:32)",
		"",
		h.Render("  Export"),
		d.Render("    w          ") + "export filtered view to capture dir",
		"",
		h.Render("  General"),
		d.Render("    ?          ") + "toggle this help",
		d.Render("    q          ") + "quit",
	}
}

// styles
var (
	headerStyle   = lipgloss.NewStyle().Bold(true)
	labelStyle    = lipgloss.NewStyle().Faint(true)
	boldStyle     = lipgloss.NewStyle().Bold(true)
	sepStyle      = lipgloss.NewStyle().Faint(true)
	logLineStyle  = lipgloss.NewStyle()
	matchStyle    = lipgloss.NewStyle().Background(lipgloss.Color("226")).Foreground(lipgloss.Color("0"))
	droppedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	warnStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("226")).Bold(true)
	searchBadge   = lipgloss.NewStyle().Background(lipgloss.Color("226")).Foreground(lipgloss.Color("0")).Padding(0, 1)
	followBadge   = lipgloss.NewStyle().Background(lipgloss.Color("34")).Foreground(lipgloss.Color("15")).Padding(0, 1)
	filterBadge   = lipgloss.NewStyle().Background(lipgloss.Color("63")).Foreground(lipgloss.Color("15")).Padding(0, 1)
	grepBadge     = lipgloss.NewStyle().Background(lipgloss.Color("28")).Foreground(lipgloss.Color("15")).Padding(0, 1)
	exportBadge   = lipgloss.NewStyle().Background(lipgloss.Color("34")).Foreground(lipgloss.Color("15")).Padding(0, 1)
	helpKeyStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("75"))
	helpDescStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
)

// helpers

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func padRight(s string, w int) string {
	n := lipgloss.Width(s)
	if n >= w {
		return s
	}
	return s + strings.Repeat(" ", w-n)
}

func padLeft(s string, w int) string {
	n := lipgloss.Width(s)
	if n >= w {
		return s
	}
	return strings.Repeat(" ", w-n) + s
}

func formatRate(r float64) string {
	switch {
	case r >= 1_000_000:
		return fmt.Sprintf("%.1fM", r/1_000_000)
	case r >= 1_000:
		return fmt.Sprintf("%.1fK", r/1_000)
	default:
		return fmt.Sprintf("%.0f", r)
	}
}

func formatBytes(b int64) string {
	switch {
	case b >= 1<<40:
		return fmt.Sprintf("%.1f TB", float64(b)/(1<<40))
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
