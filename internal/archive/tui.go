package archive

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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

	// log display
	lines       []recv.LogEntry
	scrollOff   int
	follow      bool
	ringVersion int

	// search
	searching   bool
	searchInput string
	searchRegex *regexp.Regexp
	searchIdx   int
	matches     []int

	// gg detection
	lastGPress time.Time

	// terminal size
	width  int
	height int

	// quit signal
	quitting bool
}

// NewReplayModel creates a replay TUI model.
func NewReplayModel(feeder *Feeder, ring *recv.LogRing, meta *recv.Metadata, dir string, totalLines int64) ReplayModel {
	return ReplayModel{
		feeder:     feeder,
		ring:       ring,
		meta:       meta,
		dir:        dir,
		totalLines: totalLines,
		follow:     true,
		width:      80,
		height:     24,
	}
}

// Init starts the tick timer and the feeder.
func (m ReplayModel) Init() tea.Cmd {
	m.startTime = time.Now()
	if m.feeder != nil {
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
			m.lines = m.ring.Snapshot()
			m.ringVersion = newVersion
			m.updateSearchMatches()
			if m.follow {
				m.scrollToBottom()
			}
		}

		return m, replayTickCmd()

	case tea.KeyMsg:
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

	case "/":
		m.searching = true
		m.searchInput = ""

	case "n":
		m.nextMatch(1)

	case "N":
		m.nextMatch(-1)

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

func (m ReplayModel) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.searching = false
		re, err := regexp.Compile(m.searchInput)
		if err == nil {
			m.searchRegex = re
			m.updateSearchMatches()
			m.searchIdx = 0
			if len(m.matches) > 0 {
				m.follow = false
				m.scrollOff = clamp(m.matches[0]-m.logPaneHeight()/2, 0, m.maxScroll())
			}
		}

	case "esc":
		m.searching = false
		m.searchInput = ""
		m.searchRegex = nil
		m.matches = nil

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

func (m *ReplayModel) updateSearchMatches() {
	m.matches = nil
	if m.searchRegex == nil {
		return
	}
	for i, entry := range m.lines {
		if m.searchRegex.MatchString(entry.Message) {
			m.matches = append(m.matches, i)
		}
	}
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

	// log pane
	paneH := m.logPaneHeight()
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

	// status bar
	var status strings.Builder
	if m.searching {
		status.WriteString(rSearchBadge.Render(fmt.Sprintf("/%s", m.searchInput)))
	} else if m.searchRegex != nil {
		status.WriteString(rSearchBadge.Render(fmt.Sprintf("[%d/%d] /%s", m.searchIdx+1, len(m.matches), m.searchRegex.String())))
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
	rHeaderStyle  = lipgloss.NewStyle().Bold(true)
	rLabelStyle   = lipgloss.NewStyle().Faint(true)
	rSepStyle     = lipgloss.NewStyle().Faint(true)
	rLogLineStyle = lipgloss.NewStyle()
	rMatchStyle   = lipgloss.NewStyle().Background(lipgloss.Color("226")).Foreground(lipgloss.Color("0"))
	rSearchBadge  = lipgloss.NewStyle().Background(lipgloss.Color("226")).Foreground(lipgloss.Color("0")).Padding(0, 1)
	rFollowBadge  = lipgloss.NewStyle().Background(lipgloss.Color("34")).Foreground(lipgloss.Color("15")).Padding(0, 1)
	rSpeedBadge   = lipgloss.NewStyle().Background(lipgloss.Color("33")).Foreground(lipgloss.Color("15")).Padding(0, 1)
	rPauseBadge   = lipgloss.NewStyle().Background(lipgloss.Color("208")).Foreground(lipgloss.Color("0")).Padding(0, 1)
	rDoneBadge    = lipgloss.NewStyle().Background(lipgloss.Color("34")).Foreground(lipgloss.Color("15")).Padding(0, 1)
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
