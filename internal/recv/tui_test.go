package recv

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func newTestModel() TUIModel {
	stats := NewStats()
	ring := NewLogRing(100)
	m := NewTUIModel(stats, ring, nil, 50<<30, nil, ":9000", "/tmp/test", "")
	m.width = 120
	m.height = 30
	return m
}

func sendKey(m TUIModel, key string) TUIModel {
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
	return updated.(TUIModel)
}

func sendSpecialKey(m TUIModel, key tea.KeyType) TUIModel {
	updated, _ := m.Update(tea.KeyMsg{Type: key})
	return updated.(TUIModel)
}

func feedLines(m *TUIModel, n int) {
	for i := 0; i < n; i++ {
		m.ring.Push(LogEntry{
			Timestamp: time.Date(2024, 1, 15, 10, 0, i, 0, time.UTC),
			Labels:    map[string]string{"app": "api"},
			Message:   "line",
		})
	}
	// simulate tick to pick up ring changes
	*m = applyTick(*m)
}

func applyTick(m TUIModel) TUIModel {
	updated, _ := m.Update(tickMsg(time.Now()))
	return updated.(TUIModel)
}

func TestTUIInitialState(t *testing.T) {
	m := newTestModel()
	if !m.follow {
		t.Error("expected follow mode on by default")
	}
	if m.searching {
		t.Error("expected not searching initially")
	}
	if m.quitting {
		t.Error("expected not quitting initially")
	}
}

func TestTUIQuit(t *testing.T) {
	m := newTestModel()
	m = sendKey(m, "q")
	if !m.quitting {
		t.Error("expected quitting after 'q'")
	}
}

func TestTUICtrlCQuit(t *testing.T) {
	m := newTestModel()
	m = sendSpecialKey(m, tea.KeyCtrlC)
	if !m.quitting {
		t.Error("expected quitting after ctrl+c")
	}
}

func TestTUIScrollDown(t *testing.T) {
	m := newTestModel()
	feedLines(&m, 50)
	m.scrollOff = 0
	m.follow = false

	m = sendKey(m, "j")
	if m.scrollOff != 1 {
		t.Errorf("scrollOff = %d, want 1", m.scrollOff)
	}
	if m.follow {
		t.Error("follow should be disabled on scroll")
	}
}

func TestTUIScrollUp(t *testing.T) {
	m := newTestModel()
	feedLines(&m, 50)
	m.scrollOff = 5
	m.follow = false

	m = sendKey(m, "k")
	if m.scrollOff != 4 {
		t.Errorf("scrollOff = %d, want 4", m.scrollOff)
	}
}

func TestTUIHalfPageDown(t *testing.T) {
	m := newTestModel()
	feedLines(&m, 50)
	m.scrollOff = 0
	m.follow = false

	m = sendKey(m, "d")
	half := m.logPaneHeight() / 2
	if m.scrollOff != half {
		t.Errorf("scrollOff = %d, want %d", m.scrollOff, half)
	}
}

func TestTUIHalfPageUp(t *testing.T) {
	m := newTestModel()
	feedLines(&m, 50)
	m.scrollOff = 20
	m.follow = false

	m = sendKey(m, "u")
	half := m.logPaneHeight() / 2
	expected := 20 - half
	if m.scrollOff != expected {
		t.Errorf("scrollOff = %d, want %d", m.scrollOff, expected)
	}
}

func TestTUIJumpToBottom(t *testing.T) {
	m := newTestModel()
	feedLines(&m, 50)
	m.scrollOff = 0
	m.follow = false

	m = sendKey(m, "G")
	if !m.follow {
		t.Error("expected follow after G")
	}
	if m.scrollOff != m.maxScroll() {
		t.Errorf("scrollOff = %d, want %d", m.scrollOff, m.maxScroll())
	}
}

func TestTUIToggleFollow(t *testing.T) {
	m := newTestModel()
	m.follow = true

	m = sendKey(m, "f")
	if m.follow {
		t.Error("expected follow off after toggle")
	}

	m = sendKey(m, "f")
	if !m.follow {
		t.Error("expected follow on after second toggle")
	}
}

func TestTUISearchMode(t *testing.T) {
	m := newTestModel()

	m = sendKey(m, "/")
	if !m.searching {
		t.Error("expected searching after '/'")
	}

	// type search term
	for _, c := range "hello" {
		m = sendKey(m, string(c))
	}
	if m.searchInput != "hello" {
		t.Errorf("searchInput = %q, want %q", m.searchInput, "hello")
	}

	// backspace
	m = sendSpecialKey(m, tea.KeyBackspace)
	if m.searchInput != "hell" {
		t.Errorf("searchInput = %q after backspace, want %q", m.searchInput, "hell")
	}
}

func TestTUISearchEscape(t *testing.T) {
	m := newTestModel()
	m = sendKey(m, "/")
	m = sendKey(m, "a")

	m = sendSpecialKey(m, tea.KeyEscape)
	if m.searching {
		t.Error("expected not searching after Esc")
	}
	if m.highlight != nil {
		t.Error("expected nil highlight after Esc")
	}
}

func TestTUISearchEnterAndNav(t *testing.T) {
	m := newTestModel()
	// push lines with searchable content
	for i := 0; i < 30; i++ {
		msg := "normal line"
		if i == 5 || i == 15 || i == 25 {
			msg = "MATCH here"
		}
		m.ring.Push(LogEntry{
			Timestamp: time.Date(2024, 1, 15, 10, 0, i, 0, time.UTC),
			Labels:    map[string]string{"app": "api"},
			Message:   msg,
		})
	}
	m = applyTick(m)

	m = sendKey(m, "/")
	for _, c := range "MATCH" {
		m = sendKey(m, string(c))
	}
	m = sendSpecialKey(m, tea.KeyEnter)

	if m.highlight == nil {
		t.Fatal("expected highlight after enter")
	}
	if len(m.matches) != 3 {
		t.Fatalf("matches = %d, want 3", len(m.matches))
	}

	// navigate forward
	m = sendKey(m, "n")
	if m.searchIdx != 1 {
		t.Errorf("searchIdx = %d, want 1", m.searchIdx)
	}

	// navigate backward
	m = sendKey(m, "N")
	if m.searchIdx != 0 {
		t.Errorf("searchIdx = %d, want 0", m.searchIdx)
	}

	// wrap around backward
	m = sendKey(m, "N")
	if m.searchIdx != 2 {
		t.Errorf("searchIdx = %d, want 2 (wrap)", m.searchIdx)
	}
}

func TestTUITickComputesRates(t *testing.T) {
	m := newTestModel()
	// first tick establishes baseline
	m = applyTick(m)

	// record some entries
	for i := 0; i < 100; i++ {
		m.stats.RecordEntry(map[string]string{"app": "api"})
	}

	// second tick should compute rate
	time.Sleep(10 * time.Millisecond) // ensure non-zero elapsed
	m = applyTick(m)

	if m.curr.LogsReceived != 100 {
		t.Errorf("LogsReceived = %d, want 100", m.curr.LogsReceived)
	}
	if m.logsPerSec <= 0 {
		t.Errorf("logsPerSec = %f, want > 0", m.logsPerSec)
	}
}

func TestTUIFollowAutoScrolls(t *testing.T) {
	m := newTestModel()
	m.follow = true
	feedLines(&m, 50)

	if m.scrollOff != m.maxScroll() {
		t.Errorf("scrollOff = %d, want %d (auto-scroll)", m.scrollOff, m.maxScroll())
	}
}

func TestTUIScrollClampAtZero(t *testing.T) {
	m := newTestModel()
	m.scrollOff = 0
	m.follow = false

	m = sendKey(m, "k")
	if m.scrollOff != 0 {
		t.Errorf("scrollOff = %d, want 0 (clamped)", m.scrollOff)
	}
}

func TestTUIScrollClampAtMax(t *testing.T) {
	m := newTestModel()
	feedLines(&m, 50)
	m.scrollOff = m.maxScroll()
	m.follow = false

	m = sendKey(m, "j")
	if m.scrollOff != m.maxScroll() {
		t.Errorf("scrollOff = %d, want %d (clamped)", m.scrollOff, m.maxScroll())
	}
}

func TestTUIViewRenders(t *testing.T) {
	m := newTestModel()
	feedLines(&m, 5)

	view := m.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
	if !containsStr(view, "logtap v0.1.0") {
		t.Error("expected header in view")
	}
	if !containsStr(view, "Connections:") {
		t.Error("expected stats in view")
	}
}

func TestTUIViewQuitting(t *testing.T) {
	m := newTestModel()
	m.quitting = true
	if m.View() != "" {
		t.Error("expected empty view when quitting")
	}
}

func TestTUIWindowResize(t *testing.T) {
	m := newTestModel()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 200, Height: 50})
	m = updated.(TUIModel)
	if m.width != 200 || m.height != 50 {
		t.Errorf("size = %dx%d, want 200x50", m.width, m.height)
	}
}

func TestFormatRate(t *testing.T) {
	tests := []struct {
		in   float64
		want string
	}{
		{0, "0"},
		{500, "500"},
		{1500, "1.5K"},
		{48232, "48.2K"},
		{1500000, "1.5M"},
	}
	for _, tt := range tests {
		got := formatRate(tt.in)
		if got != tt.want {
			t.Errorf("formatRate(%f) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1 << 10, "1.0 KB"},
		{62 << 20, "62.0 MB"},
		{14<<30 + 200<<20, "14.2 GB"},
		{1 << 40, "1.0 TB"},
	}
	for _, tt := range tests {
		got := formatBytes(tt.in)
		if got != tt.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestTUILabelFilterMode(t *testing.T) {
	m := newTestModel()

	m = sendKey(m, "l")
	if !m.filtering {
		t.Error("expected filtering after 'l'")
	}

	// type filter
	for _, c := range "app=api" {
		m = sendKey(m, string(c))
	}
	if m.filterInput != "app=api" {
		t.Errorf("filterInput = %q, want %q", m.filterInput, "app=api")
	}

	// enter applies
	m = sendSpecialKey(m, tea.KeyEnter)
	if m.filtering {
		t.Error("expected not filtering after enter")
	}
	if !m.filterActive {
		t.Error("expected filterActive after enter")
	}
	if m.filterKey != "app" || m.filterVal != "api" {
		t.Errorf("filter = %s=%s, want app=api", m.filterKey, m.filterVal)
	}
}

func TestTUILabelFilterEscape(t *testing.T) {
	m := newTestModel()
	m = sendKey(m, "l")
	m = sendKey(m, "a")

	m = sendSpecialKey(m, tea.KeyEscape)
	if m.filtering {
		t.Error("expected not filtering after Esc")
	}
	if m.filterActive {
		t.Error("expected filter cleared after Esc")
	}
}

func TestTUILabelFilterClearOnEmpty(t *testing.T) {
	m := newTestModel()

	// set a filter first
	m = sendKey(m, "l")
	for _, c := range "app=api" {
		m = sendKey(m, string(c))
	}
	m = sendSpecialKey(m, tea.KeyEnter)
	if !m.filterActive {
		t.Fatal("expected filter active")
	}

	// now enter empty filter to clear
	m = sendKey(m, "l")
	m = sendSpecialKey(m, tea.KeyEnter)
	if m.filterActive {
		t.Error("expected filter cleared on empty input")
	}
}

func TestTUILabelFilterFiltersLines(t *testing.T) {
	m := newTestModel()

	// push mixed entries
	m.ring.Push(LogEntry{
		Timestamp: time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
		Labels:    map[string]string{"app": "api"},
		Message:   "api line",
	})
	m.ring.Push(LogEntry{
		Timestamp: time.Date(2024, 1, 15, 10, 0, 1, 0, time.UTC),
		Labels:    map[string]string{"app": "web"},
		Message:   "web line",
	})
	m.ring.Push(LogEntry{
		Timestamp: time.Date(2024, 1, 15, 10, 0, 2, 0, time.UTC),
		Labels:    map[string]string{"app": "api"},
		Message:   "api line 2",
	})
	m = applyTick(m)
	if len(m.lines) != 3 {
		t.Fatalf("unfiltered lines = %d, want 3", len(m.lines))
	}

	// apply filter
	m = sendKey(m, "l")
	for _, c := range "app=api" {
		m = sendKey(m, string(c))
	}
	m = sendSpecialKey(m, tea.KeyEnter)
	m = applyTick(m) // force re-read

	if len(m.lines) != 2 {
		t.Errorf("filtered lines = %d, want 2", len(m.lines))
	}
}

func TestTUILabelFilterBadge(t *testing.T) {
	m := newTestModel()
	feedLines(&m, 5)

	// set filter
	m = sendKey(m, "l")
	for _, c := range "app=api" {
		m = sendKey(m, string(c))
	}
	m = sendSpecialKey(m, tea.KeyEnter)
	m = applyTick(m)

	view := m.View()
	if !containsStr(view, "FILTER: app=api") {
		t.Error("expected filter badge in view")
	}
}

func TestTUILabelFilterBackspace(t *testing.T) {
	m := newTestModel()
	m = sendKey(m, "l")
	m = sendKey(m, "a")
	m = sendKey(m, "p")
	m = sendSpecialKey(m, tea.KeyBackspace)
	if m.filterInput != "a" {
		t.Errorf("filterInput = %q after backspace, want %q", m.filterInput, "a")
	}
}

func TestTUIFilterStackHide(t *testing.T) {
	m := newTestModel()
	m.ring.Push(LogEntry{Timestamp: time.Now(), Labels: map[string]string{"app": "api"}, Message: "error in api"})
	m.ring.Push(LogEntry{Timestamp: time.Now(), Labels: map[string]string{"app": "api"}, Message: "linkerd proxy"})
	m.ring.Push(LogEntry{Timestamp: time.Now(), Labels: map[string]string{"app": "api"}, Message: "healthz ok"})
	m.ring.Push(LogEntry{Timestamp: time.Now(), Labels: map[string]string{"app": "api"}, Message: "real error"})
	m = applyTick(m)
	if len(m.lines) != 4 {
		t.Fatalf("lines = %d, want 4", len(m.lines))
	}

	// hide linkerd
	m = sendKey(m, "/")
	for _, c := range "!linkerd" {
		m = sendKey(m, string(c))
	}
	m = sendSpecialKey(m, tea.KeyEnter)
	m = applyTick(m)
	if len(m.lines) != 3 {
		t.Errorf("after hide linkerd: lines = %d, want 3", len(m.lines))
	}
	if len(m.filterStack) != 1 {
		t.Fatalf("filterStack = %d, want 1", len(m.filterStack))
	}

	// stack: also hide healthz
	m = sendKey(m, "/")
	for _, c := range "!healthz" {
		m = sendKey(m, string(c))
	}
	m = sendSpecialKey(m, tea.KeyEnter)
	m = applyTick(m)
	if len(m.lines) != 2 {
		t.Errorf("after hide healthz: lines = %d, want 2", len(m.lines))
	}
	if len(m.filterStack) != 2 {
		t.Fatalf("filterStack = %d, want 2", len(m.filterStack))
	}

	// Esc pops last filter (healthz) — should go back to 3 lines
	m = sendSpecialKey(m, tea.KeyEscape)
	m = applyTick(m)
	if len(m.filterStack) != 1 {
		t.Fatalf("after pop: filterStack = %d, want 1", len(m.filterStack))
	}
	if len(m.lines) != 3 {
		t.Errorf("after pop: lines = %d, want 3", len(m.lines))
	}

	// Esc pops last filter (linkerd) — should go back to 4 lines
	m = sendSpecialKey(m, tea.KeyEscape)
	m = applyTick(m)
	if len(m.filterStack) != 0 {
		t.Fatalf("after pop all: filterStack = %d, want 0", len(m.filterStack))
	}
	if len(m.lines) != 4 {
		t.Errorf("after pop all: lines = %d, want 4", len(m.lines))
	}
}

func TestTUIFilterStackGrep(t *testing.T) {
	m := newTestModel()
	m.ring.Push(LogEntry{Timestamp: time.Now(), Labels: map[string]string{"app": "api"}, Message: "error in api"})
	m.ring.Push(LogEntry{Timestamp: time.Now(), Labels: map[string]string{"app": "api"}, Message: "linkerd proxy"})
	m.ring.Push(LogEntry{Timestamp: time.Now(), Labels: map[string]string{"app": "api"}, Message: "error critical"})
	m = applyTick(m)

	// hide linkerd, then grep error
	m = sendKey(m, "/")
	for _, c := range "!linkerd" {
		m = sendKey(m, string(c))
	}
	m = sendSpecialKey(m, tea.KeyEnter)

	m = sendKey(m, "/")
	for _, c := range "=error" {
		m = sendKey(m, string(c))
	}
	m = sendSpecialKey(m, tea.KeyEnter)
	m = applyTick(m)

	if len(m.filterStack) != 2 {
		t.Fatalf("filterStack = %d, want 2", len(m.filterStack))
	}
	if len(m.lines) != 2 {
		t.Errorf("after hide+grep: lines = %d, want 2", len(m.lines))
	}
}

func TestTUIFilterStackHighlightSeparate(t *testing.T) {
	m := newTestModel()
	m.ring.Push(LogEntry{Timestamp: time.Now(), Labels: map[string]string{"app": "api"}, Message: "error in api"})
	m.ring.Push(LogEntry{Timestamp: time.Now(), Labels: map[string]string{"app": "api"}, Message: "linkerd proxy"})
	m.ring.Push(LogEntry{Timestamp: time.Now(), Labels: map[string]string{"app": "api"}, Message: "error critical"})
	m = applyTick(m)

	// hide linkerd
	m = sendKey(m, "/")
	for _, c := range "!linkerd" {
		m = sendKey(m, string(c))
	}
	m = sendSpecialKey(m, tea.KeyEnter)
	m = applyTick(m)

	// highlight "critical" — should not push onto stack
	m = sendKey(m, "/")
	for _, c := range "critical" {
		m = sendKey(m, string(c))
	}
	m = sendSpecialKey(m, tea.KeyEnter)

	if len(m.filterStack) != 1 {
		t.Errorf("filterStack = %d, want 1 (highlight should not push)", len(m.filterStack))
	}
	if m.highlight == nil {
		t.Error("expected highlight to be set")
	}
	if len(m.matches) != 1 {
		t.Errorf("matches = %d, want 1", len(m.matches))
	}

	// Esc clears highlight first, stack stays
	m = sendSpecialKey(m, tea.KeyEscape)
	if m.highlight != nil {
		t.Error("expected highlight cleared after Esc")
	}
	if len(m.filterStack) != 1 {
		t.Error("expected stack preserved after clearing highlight")
	}
}

func TestFindTimeIndex(t *testing.T) {
	lines := []LogEntry{
		{Timestamp: time.Date(2026, 3, 5, 14, 30, 0, 0, time.UTC)},
		{Timestamp: time.Date(2026, 3, 5, 14, 31, 0, 0, time.UTC)},
		{Timestamp: time.Date(2026, 3, 5, 14, 32, 0, 0, time.UTC)},
		{Timestamp: time.Date(2026, 3, 5, 14, 32, 30, 0, time.UTC)},
		{Timestamp: time.Date(2026, 3, 5, 14, 33, 0, 0, time.UTC)},
	}

	tests := []struct {
		input string
		want  int
	}{
		{"14:32", 2},
		{"14:32:30", 3},
		{"14:30", 0},
		{"14:33", 4},
		{"14:35", -1},
		{"", -1},
	}
	for _, tt := range tests {
		got := FindTimeIndex(lines, tt.input)
		if got != tt.want {
			t.Errorf("FindTimeIndex(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestTUITimeJumpMode(t *testing.T) {
	m := newTestModel()
	for i := 0; i < 50; i++ {
		m.ring.Push(LogEntry{
			Timestamp: time.Date(2026, 3, 5, 14, 30, i, 0, time.UTC),
			Labels:    map[string]string{"app": "api"},
			Message:   "line",
		})
	}
	m = applyTick(m)

	m = sendKey(m, "t")
	if !m.timeJumping {
		t.Error("expected timeJumping after 't'")
	}

	for _, c := range "14:30:25" {
		m = sendKey(m, string(c))
	}
	if m.timeJumpInput != "14:30:25" {
		t.Errorf("timeJumpInput = %q, want %q", m.timeJumpInput, "14:30:25")
	}

	m = sendSpecialKey(m, tea.KeyEnter)
	if m.timeJumping {
		t.Error("expected not timeJumping after enter")
	}
	if m.follow {
		t.Error("expected follow disabled after time jump")
	}
}

func TestTUITimeJumpEscape(t *testing.T) {
	m := newTestModel()
	m = sendKey(m, "t")
	m = sendKey(m, "1")
	m = sendSpecialKey(m, tea.KeyEscape)
	if m.timeJumping {
		t.Error("expected not timeJumping after Esc")
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && searchStr(s, sub)
}

func searchStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
