package archive

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/ppiankov/logtap/internal/recv"
)

type replayTickMsg time.Time

func replayTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return replayTickMsg(t)
	})
}

// ReplayModel is the bubbletea model for the capture replay TUI.
type ReplayModel struct {
	feeder *Feeder
	ring   *recv.LogRing
	meta   *recv.Metadata
	dir    string

	totalLines int64
	startTime  time.Time

	// service picker
	picker       bool // true while picker is shown
	services     []ServiceEntry
	pickerSel    []bool // selection state per service
	pickerCursor int

	// log display
	lines       []recv.LogEntry
	scrollOff   int
	follow      bool
	ringVersion int

	// search — filter stack
	searching   bool
	searchInput string
	filterStack []recv.SearchFilter // stacked hide/grep filters
	highlight   *regexp.Regexp      // current highlight (not part of stack)
	searchIdx   int
	matches     []int

	// label filter
	filtering    bool
	filterInput  string
	filterKey    string
	filterVal    string
	filterActive bool

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

// NewReplayModel creates a replay TUI model.
// If services has >1 entry, a picker is shown before replay starts.
func NewReplayModel(feeder *Feeder, ring *recv.LogRing, meta *recv.Metadata, dir string, totalLines int64, services []ServiceEntry) ReplayModel {
	lipgloss.SetColorProfile(termenv.ANSI256)

	showPicker := len(services) > 1
	sel := make([]bool, len(services))
	for i := range sel {
		sel[i] = true // default: all selected
	}

	return ReplayModel{
		feeder:     feeder,
		ring:       ring,
		meta:       meta,
		dir:        dir,
		totalLines: totalLines,
		picker:     showPicker,
		services:   services,
		pickerSel:  sel,
		follow:     true,
		width:      80,
		height:     24,
	}
}

// Init starts the tick timer and the feeder (unless picker is shown first).
func (m ReplayModel) Init() tea.Cmd {
	if !m.picker && m.feeder != nil {
		m.feeder.Start()
	}
	return replayTickCmd()
}

// Update handles messages.
func (m ReplayModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case replayTickMsg:
		if m.startTime.IsZero() {
			m.startTime = time.Time(msg)
		}

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

		return m, replayTickCmd()

	case tea.KeyMsg:
		if m.picker {
			return m.updatePicker(msg)
		}
		if m.showHelp {
			if msg.String() == "?" || msg.String() == "esc" || msg.String() == "q" {
				m.showHelp = false
			}
			return m, nil
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

func (m ReplayModel) updateNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		m.quitting = true
		if m.feeder != nil {
			m.feeder.Stop()
		}
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

	case "?":
		m.showHelp = !m.showHelp

	case " ":
		if m.feeder != nil {
			m.feeder.TogglePause()
		}

	case "]":
		if m.feeder != nil {
			s := m.feeder.Speed()
			if s == 0 {
				// already instant, no faster
			} else if s < 1 {
				m.feeder.SetSpeed(SpeedRealtime)
			} else {
				m.feeder.SetSpeed(s * 2)
			}
		}

	case "[":
		if m.feeder != nil {
			s := m.feeder.Speed()
			if s == 0 {
				m.feeder.SetSpeed(Speed(64))
			} else if s <= 1 {
				// already at or below realtime, don't go below 0.5x
				m.feeder.SetSpeed(s / 2)
			} else {
				newSpeed := s / 2
				if newSpeed < 1 {
					newSpeed = 1
				}
				m.feeder.SetSpeed(newSpeed)
			}
		}
	}

	return m, nil
}

func (m ReplayModel) updatePicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		m.quitting = true
		return m, tea.Quit

	case "j", "down":
		if m.pickerCursor < len(m.services)-1 {
			m.pickerCursor++
		}

	case "k", "up":
		if m.pickerCursor > 0 {
			m.pickerCursor--
		}

	case " ":
		if m.pickerCursor < len(m.pickerSel) {
			m.pickerSel[m.pickerCursor] = !m.pickerSel[m.pickerCursor]
		}

	case "a":
		// toggle all
		allSelected := true
		for _, s := range m.pickerSel {
			if !s {
				allSelected = false
				break
			}
		}
		for i := range m.pickerSel {
			m.pickerSel[i] = !allSelected
		}

	case "enter":
		// build label filter from selection
		selected := make(map[string]bool)
		for i, svc := range m.services {
			if m.pickerSel[i] {
				selected[svc.Value] = true
			}
		}
		if len(selected) > 0 && len(selected) < len(m.services) {
			// set label predicate on feeder
			labelKey := m.services[0].Label
			if m.feeder != nil {
				m.feeder.SetLabelFilter(func(e recv.LogEntry) bool {
					v, ok := e.Labels[labelKey]
					return ok && selected[v]
				})
			}
		}
		m.picker = false
		if m.feeder != nil {
			m.feeder.Start()
		}
	}

	return m, nil
}

func (m ReplayModel) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.searching = false
		input := m.searchInput
		if strings.HasPrefix(input, "!") {
			re, err := regexp.Compile(input[1:])
			if err == nil {
				m.filterStack = append(m.filterStack, recv.SearchFilter{Regex: re, Mode: recv.ModeHide})
				m.applySearchFilter()
				m.scrollOff = 0
				if m.follow {
					m.scrollToBottom()
				}
			}
		} else if strings.HasPrefix(input, "=") {
			re, err := regexp.Compile(input[1:])
			if err == nil {
				m.filterStack = append(m.filterStack, recv.SearchFilter{Regex: re, Mode: recv.ModeGrep})
				m.applySearchFilter()
				m.scrollOff = 0
				if m.follow {
					m.scrollToBottom()
				}
			}
		} else {
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
func (m *ReplayModel) applySearchFilter() {
	for _, f := range m.filterStack {
		switch f.Mode {
		case recv.ModeGrep:
			filtered := make([]recv.LogEntry, 0)
			for _, entry := range m.lines {
				if recv.EntryMatchesRegex(entry, f.Regex) {
					filtered = append(filtered, entry)
				}
			}
			m.lines = filtered
		case recv.ModeHide:
			filtered := make([]recv.LogEntry, 0, len(m.lines))
			for _, entry := range m.lines {
				if !recv.EntryMatchesRegex(entry, f.Regex) {
					filtered = append(filtered, entry)
				}
			}
			m.lines = filtered
		}
	}
}

func (m *ReplayModel) updateSearchMatches() {
	m.matches = nil
	if m.highlight == nil {
		return
	}
	for i, entry := range m.lines {
		if recv.EntryMatchesRegex(entry, m.highlight) {
			m.matches = append(m.matches, i)
		}
	}
}

func (m ReplayModel) updateFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
		m.ringVersion = -1

	case "esc":
		m.filtering = false
		m.filterInput = ""
		m.filterActive = false
		m.filterKey = ""
		m.filterVal = ""
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

func (m ReplayModel) labelPredicate() func(recv.LogEntry) bool {
	key, val := m.filterKey, m.filterVal
	return func(e recv.LogEntry) bool {
		v, ok := e.Labels[key]
		return ok && v == val
	}
}

func (m ReplayModel) renderHelp() []string {
	h := rHelpKeyStyle
	d := rHelpDescStyle
	return []string{
		rBoldStyle.Render("  Keybindings") + rLabelStyle.Render("  (press ? or Esc to close)"),
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
		"",
		h.Render("  Playback"),
		d.Render("    Space      ") + "pause/resume",
		d.Render("    [/]        ") + "decrease/increase speed",
		"",
		h.Render("  General"),
		d.Render("    ?          ") + "toggle this help",
		d.Render("    q          ") + "quit",
	}
}

func (m ReplayModel) renderPicker() string {
	var b strings.Builder

	b.WriteString(rBoldStyle.Render("  Select services to load"))
	b.WriteString("\n\n")
	b.WriteString(rLabelStyle.Render(fmt.Sprintf("  Capture: %s", m.dir)))
	b.WriteString("\n")

	timeRange := ""
	if !m.meta.Started.IsZero() {
		start := m.meta.Started.Format("2006-01-15 15:04")
		if !m.meta.Stopped.IsZero() {
			stop := m.meta.Stopped.Format("15:04")
			timeRange = fmt.Sprintf("%s — %s", start, stop)
		} else {
			timeRange = start
		}
	}
	if timeRange != "" {
		b.WriteString(rLabelStyle.Render(fmt.Sprintf("  Time:    %s", timeRange)))
		b.WriteString("\n")
	}
	b.WriteString(rLabelStyle.Render(fmt.Sprintf("  Total:   %s lines", formatRate(float64(m.totalLines)))))
	b.WriteString("\n\n")

	for i, svc := range m.services {
		cursor := "  "
		if i == m.pickerCursor {
			cursor = "> "
		}
		check := "[ ]"
		if m.pickerSel[i] {
			check = "[x]"
		}
		line := fmt.Sprintf("%s%s %s (%s lines)", cursor, check, svc.Value, formatRate(float64(svc.Lines)))
		if i == m.pickerCursor {
			b.WriteString(rPickerCursorStyle.Render(line))
		} else if m.pickerSel[i] {
			b.WriteString(rPickerSelStyle.Render(line))
		} else {
			b.WriteString(line)
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(rLabelStyle.Render("  Space: toggle  |  a: toggle all  |  Enter: confirm  |  q: quit"))
	b.WriteString("\n")

	return b.String()
}

func (m *ReplayModel) nextMatch(dir int) {
	if len(m.matches) == 0 {
		return
	}
	m.searchIdx = (m.searchIdx + dir + len(m.matches)) % len(m.matches)
	target := m.matches[m.searchIdx]
	m.follow = false
	m.scrollOff = clamp(target-m.logPaneHeight()/2, 0, m.maxScroll())
}

func (m *ReplayModel) scrollToBottom() {
	m.scrollOff = m.maxScroll()
}

func (m ReplayModel) logPaneHeight() int {
	// header(1) + blank(1) + progress(4) + separator(1) = 7 lines overhead
	h := m.height - 7
	if h < 1 {
		h = 1
	}
	return h
}

func (m ReplayModel) maxScroll() int {
	max := len(m.lines) - m.logPaneHeight()
	if max < 0 {
		return 0
	}
	return max
}

// View renders the replay TUI.
func (m ReplayModel) View() string {
	if m.quitting {
		return ""
	}

	if m.picker {
		return m.renderPicker()
	}

	var b strings.Builder

	// header
	timeRange := ""
	if !m.meta.Started.IsZero() {
		start := m.meta.Started.Format("2006-01-15 15:04")
		if !m.meta.Stopped.IsZero() {
			stop := m.meta.Stopped.Format("15:04")
			timeRange = fmt.Sprintf(" | %s — %s", start, stop)
		} else {
			timeRange = fmt.Sprintf(" | %s", start)
		}
	}
	header := rHeaderStyle.Render(fmt.Sprintf("logtap open | %s%s | %s lines",
		m.dir, timeRange, formatRate(float64(m.totalLines))))
	b.WriteString(header)
	b.WriteString("\n\n")

	// progress section
	var emitted int64
	var speed Speed
	var paused, done bool
	if m.feeder != nil {
		emitted = m.feeder.LinesEmitted()
		speed = m.feeder.Speed()
		paused = m.feeder.Paused()
		done = m.feeder.Done()
	}

	pct := float64(0)
	if m.totalLines > 0 {
		pct = float64(emitted) / float64(m.totalLines) * 100
		if pct > 100 {
			pct = 100
		}
	}

	barWidth := m.width - 16
	if barWidth < 10 {
		barWidth = 10
	}
	filled := int(pct / 100 * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}
	bar := strings.Repeat("=", filled) + strings.Repeat("-", barWidth-filled)

	b.WriteString(rLabelStyle.Render(" Progress:  "))
	b.WriteString(fmt.Sprintf("[%s] %.0f%%\n", bar, pct))

	b.WriteString(rLabelStyle.Render(" Lines:     "))
	b.WriteString(fmt.Sprintf("%s / %s\n", formatRate(float64(emitted)), formatRate(float64(m.totalLines))))

	elapsed := time.Since(m.startTime)
	b.WriteString(rLabelStyle.Render(" Speed:     "))
	speedStr := formatSpeed(speed)
	if paused {
		speedStr = "PAUSED"
	} else if done {
		speedStr = fmt.Sprintf("DONE in %s", formatDuration(elapsed))
	}
	b.WriteString(speedStr)
	b.WriteString("\n")

	// separator
	b.WriteString(rSepStyle.Render(strings.Repeat("─", m.width)))
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
				b.WriteString(rMatchStyle.Render(line))
			} else {
				b.WriteString(rLogLineStyle.Render(line))
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
	if m.filtering {
		status.WriteString(rFilterBadge.Render(fmt.Sprintf("filter: %s", m.filterInput)))
	} else if m.filterActive {
		status.WriteString(rFilterBadge.Render(fmt.Sprintf("FILTER: %s=%s", m.filterKey, m.filterVal)))
	}
	// filter stack badges
	for _, f := range m.filterStack {
		if status.Len() > 0 {
			status.WriteString(" ")
		}
		switch f.Mode {
		case recv.ModeHide:
			status.WriteString(rFilterBadge.Render(fmt.Sprintf("HIDE: /%s", f.Regex.String())))
		case recv.ModeGrep:
			status.WriteString(rGrepBadge.Render(fmt.Sprintf("GREP: /%s", f.Regex.String())))
		}
	}
	if len(m.filterStack) > 0 {
		if status.Len() > 0 {
			status.WriteString(" ")
		}
		status.WriteString(rFilterBadge.Render(fmt.Sprintf("[%d lines]", len(m.lines))))
	}
	if m.searching {
		if status.Len() > 0 {
			status.WriteString(" ")
		}
		status.WriteString(rSearchBadge.Render(fmt.Sprintf("/%s", m.searchInput)))
	} else if m.highlight != nil {
		if status.Len() > 0 {
			status.WriteString(" ")
		}
		status.WriteString(rSearchBadge.Render(fmt.Sprintf("[%d/%d] /%s", m.searchIdx+1, len(m.matches), m.highlight.String())))
	}

	// speed badge
	if !done {
		if status.Len() > 0 {
			status.WriteString(" ")
		}
		if paused {
			status.WriteString(rPauseBadge.Render("PAUSED"))
		} else {
			status.WriteString(rSpeedBadge.Render(formatSpeed(speed)))
		}
	} else {
		if status.Len() > 0 {
			status.WriteString(" ")
		}
		status.WriteString(rDoneBadge.Render("COMPLETE"))
	}

	if m.follow {
		if status.Len() > 0 {
			status.WriteString(" ")
		}
		status.WriteString(rFollowBadge.Render("FOLLOW"))
	}

	if status.Len() > 0 {
		b.WriteString(padLeft(status.String(), m.width))
	}

	return b.String()
}

// styles
var (
	rHeaderStyle       = lipgloss.NewStyle().Bold(true)
	rLabelStyle        = lipgloss.NewStyle().Faint(true)
	rBoldStyle         = lipgloss.NewStyle().Bold(true)
	rSepStyle          = lipgloss.NewStyle().Faint(true)
	rLogLineStyle      = lipgloss.NewStyle()
	rMatchStyle        = lipgloss.NewStyle().Background(lipgloss.Color("226")).Foreground(lipgloss.Color("0"))
	rSearchBadge       = lipgloss.NewStyle().Background(lipgloss.Color("226")).Foreground(lipgloss.Color("0")).Padding(0, 1)
	rFollowBadge       = lipgloss.NewStyle().Background(lipgloss.Color("34")).Foreground(lipgloss.Color("15")).Padding(0, 1)
	rFilterBadge       = lipgloss.NewStyle().Background(lipgloss.Color("63")).Foreground(lipgloss.Color("15")).Padding(0, 1)
	rGrepBadge         = lipgloss.NewStyle().Background(lipgloss.Color("28")).Foreground(lipgloss.Color("15")).Padding(0, 1)
	rSpeedBadge        = lipgloss.NewStyle().Background(lipgloss.Color("33")).Foreground(lipgloss.Color("15")).Padding(0, 1)
	rPauseBadge        = lipgloss.NewStyle().Background(lipgloss.Color("208")).Foreground(lipgloss.Color("0")).Padding(0, 1)
	rDoneBadge         = lipgloss.NewStyle().Background(lipgloss.Color("34")).Foreground(lipgloss.Color("15")).Padding(0, 1)
	rHelpKeyStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("75"))
	rHelpDescStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	rPickerCursorStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("75"))
	rPickerSelStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("34"))
)

// helpers (redeclared from recv/tui.go — acceptable for two consumers)

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
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

func formatSpeed(s Speed) string {
	if s == 0 {
		return "instant"
	}
	if s == 1 {
		return "1x"
	}
	if s == Speed(int(s)) {
		return fmt.Sprintf("%dx", int(s))
	}
	return fmt.Sprintf("%.1fx", float64(s))
}

func formatDuration(d time.Duration) string {
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}
