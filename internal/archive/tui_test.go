package archive

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ppiankov/logtap/internal/recv"
)

func newTestReplayModel() ReplayModel {
	ring := recv.NewLogRing(100)
	meta := &recv.Metadata{
		Version: 1,
		Format:  "jsonl",
		Started: time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
		Stopped: time.Date(2024, 1, 15, 10, 45, 0, 0, time.UTC),
	}
	m := NewReplayModel(nil, ring, meta, "/tmp/capture", 1200000)
	m.width = 120
	m.height = 30
	m.startTime = time.Now()
	return m
}

func sendReplayKey(m ReplayModel, key string) ReplayModel {
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
	return updated.(ReplayModel)
}

func sendReplaySpecialKey(m ReplayModel, key tea.KeyType) ReplayModel {
	updated, _ := m.Update(tea.KeyMsg{Type: key})
	return updated.(ReplayModel)
}

func feedReplayLines(m *ReplayModel, n int) {
	for i := 0; i < n; i++ {
		m.ring.Push(recv.LogEntry{
			Timestamp: time.Date(2024, 1, 15, 10, 0, i, 0, time.UTC),
			Labels:    map[string]string{"app": "api"},
			Message:   "line",
		})
	}
	*m = applyReplayTick(*m)
}

func applyReplayTick(m ReplayModel) ReplayModel {
	updated, _ := m.Update(replayTickMsg(time.Now()))
	return updated.(ReplayModel)
}

func TestReplayInitialState(t *testing.T) {
	m := newTestReplayModel()
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

func TestReplayQuit(t *testing.T) {
	m := newTestReplayModel()
	m = sendReplayKey(m, "q")
	if !m.quitting {
		t.Error("expected quitting after 'q'")
	}
}

func TestReplayCtrlCQuit(t *testing.T) {
	m := newTestReplayModel()
	m = sendReplaySpecialKey(m, tea.KeyCtrlC)
	if !m.quitting {
		t.Error("expected quitting after ctrl+c")
	}
}

func TestReplayScrollDown(t *testing.T) {
	m := newTestReplayModel()
	feedReplayLines(&m, 50)
	m.scrollOff = 0
	m.follow = false

	m = sendReplayKey(m, "j")
	if m.scrollOff != 1 {
		t.Errorf("scrollOff = %d, want 1", m.scrollOff)
	}
	if m.follow {
		t.Error("follow should be disabled on scroll")
	}
}

func TestReplayScrollUp(t *testing.T) {
	m := newTestReplayModel()
	feedReplayLines(&m, 50)
	m.scrollOff = 5
	m.follow = false

	m = sendReplayKey(m, "k")
	if m.scrollOff != 4 {
		t.Errorf("scrollOff = %d, want 4", m.scrollOff)
	}
}

func TestReplayHalfPageDown(t *testing.T) {
	m := newTestReplayModel()
	feedReplayLines(&m, 50)
	m.scrollOff = 0
	m.follow = false

	m = sendReplayKey(m, "d")
	half := m.logPaneHeight() / 2
	if m.scrollOff != half {
		t.Errorf("scrollOff = %d, want %d", m.scrollOff, half)
	}
}

func TestReplayHalfPageUp(t *testing.T) {
	m := newTestReplayModel()
	feedReplayLines(&m, 50)
	m.scrollOff = 20
	m.follow = false

	m = sendReplayKey(m, "u")
	half := m.logPaneHeight() / 2
	expected := 20 - half
	if m.scrollOff != expected {
		t.Errorf("scrollOff = %d, want %d", m.scrollOff, expected)
	}
}

func TestReplayJumpToBottom(t *testing.T) {
	m := newTestReplayModel()
	feedReplayLines(&m, 50)
	m.scrollOff = 0
	m.follow = false

	m = sendReplayKey(m, "G")
	if !m.follow {
		t.Error("expected follow after G")
	}
	if m.scrollOff != m.maxScroll() {
		t.Errorf("scrollOff = %d, want %d", m.scrollOff, m.maxScroll())
	}
}

func TestReplayToggleFollow(t *testing.T) {
	m := newTestReplayModel()
	m.follow = true

	m = sendReplayKey(m, "f")
	if m.follow {
		t.Error("expected follow off after toggle")
	}

	m = sendReplayKey(m, "f")
	if !m.follow {
		t.Error("expected follow on after second toggle")
	}
}

func TestReplaySearchMode(t *testing.T) {
	m := newTestReplayModel()

	m = sendReplayKey(m, "/")
	if !m.searching {
		t.Error("expected searching after '/'")
	}

	for _, c := range "hello" {
		m = sendReplayKey(m, string(c))
	}
	if m.searchInput != "hello" {
		t.Errorf("searchInput = %q, want %q", m.searchInput, "hello")
	}

	m = sendReplaySpecialKey(m, tea.KeyBackspace)
	if m.searchInput != "hell" {
		t.Errorf("searchInput = %q after backspace, want %q", m.searchInput, "hell")
	}
}

func TestReplaySearchEscape(t *testing.T) {
	m := newTestReplayModel()
	m = sendReplayKey(m, "/")
	m = sendReplayKey(m, "a")

	m = sendReplaySpecialKey(m, tea.KeyEscape)
	if m.searching {
		t.Error("expected not searching after Esc")
	}
	if m.searchRegex != nil {
		t.Error("expected nil searchRegex after Esc")
	}
}

func TestReplaySearchEnterAndNav(t *testing.T) {
	m := newTestReplayModel()
	for i := 0; i < 30; i++ {
		msg := "normal line"
		if i == 5 || i == 15 || i == 25 {
			msg = "MATCH here"
		}
		m.ring.Push(recv.LogEntry{
			Timestamp: time.Date(2024, 1, 15, 10, 0, i, 0, time.UTC),
			Labels:    map[string]string{"app": "api"},
			Message:   msg,
		})
	}
	m = applyReplayTick(m)

	m = sendReplayKey(m, "/")
	for _, c := range "MATCH" {
		m = sendReplayKey(m, string(c))
	}
	m = sendReplaySpecialKey(m, tea.KeyEnter)

	if m.searchRegex == nil {
		t.Fatal("expected searchRegex after enter")
	}
	if len(m.matches) != 3 {
		t.Fatalf("matches = %d, want 3", len(m.matches))
	}

	m = sendReplayKey(m, "n")
	if m.searchIdx != 1 {
		t.Errorf("searchIdx = %d, want 1", m.searchIdx)
	}

	m = sendReplayKey(m, "N")
	if m.searchIdx != 0 {
		t.Errorf("searchIdx = %d, want 0", m.searchIdx)
	}

	m = sendReplayKey(m, "N")
	if m.searchIdx != 2 {
		t.Errorf("searchIdx = %d, want 2 (wrap)", m.searchIdx)
	}
}

func TestReplaySpacePause(t *testing.T) {
	_, reader := setupFeederDir(t, 100, time.Second)
	ring := recv.NewLogRing(200)
	feeder := NewFeeder(reader, ring, nil, SpeedRealtime)

	meta := &recv.Metadata{Version: 1, Format: "jsonl"}
	m := NewReplayModel(feeder, ring, meta, "/tmp/test", 100)
	m.width = 120
	m.height = 30
	m.startTime = time.Now()

	// space toggles pause
	m = sendReplayKey(m, " ")
	if !feeder.Paused() {
		t.Error("expected paused after space")
	}

	m = sendReplayKey(m, " ")
	if feeder.Paused() {
		t.Error("expected unpaused after second space")
	}
}

func TestReplaySpeedBrackets(t *testing.T) {
	_, reader := setupFeederDir(t, 100, time.Second)
	ring := recv.NewLogRing(200)
	feeder := NewFeeder(reader, ring, nil, SpeedRealtime)

	meta := &recv.Metadata{Version: 1, Format: "jsonl"}
	m := NewReplayModel(feeder, ring, meta, "/tmp/test", 100)
	m.width = 120
	m.height = 30
	m.startTime = time.Now()

	// ] speeds up
	m = sendReplayKey(m, "]")
	if feeder.Speed() != 2 {
		t.Errorf("Speed = %v, want 2 after ]", feeder.Speed())
	}

	m = sendReplayKey(m, "]")
	if feeder.Speed() != 4 {
		t.Errorf("Speed = %v, want 4 after ]]", feeder.Speed())
	}

	// [ slows down
	m = sendReplayKey(m, "[")
	if feeder.Speed() != 2 {
		t.Errorf("Speed = %v, want 2 after [", feeder.Speed())
	}

	m = sendReplayKey(m, "[")
	if feeder.Speed() != 1 {
		t.Errorf("Speed = %v, want 1 after [[", feeder.Speed())
	}
}

func TestReplayViewRenders(t *testing.T) {
	m := newTestReplayModel()
	feedReplayLines(&m, 5)

	view := m.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
	if !strings.Contains(view, "logtap open") {
		t.Error("expected header in view")
	}
	if !strings.Contains(view, "Progress:") {
		t.Error("expected progress in view")
	}
	if !strings.Contains(view, "Lines:") {
		t.Error("expected lines stat in view")
	}
	if !strings.Contains(view, "Speed:") {
		t.Error("expected speed stat in view")
	}
}

func TestReplayViewQuitting(t *testing.T) {
	m := newTestReplayModel()
	m.quitting = true
	if m.View() != "" {
		t.Error("expected empty view when quitting")
	}
}

func TestReplayWindowResize(t *testing.T) {
	m := newTestReplayModel()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 200, Height: 50})
	m = updated.(ReplayModel)
	if m.width != 200 || m.height != 50 {
		t.Errorf("size = %dx%d, want 200x50", m.width, m.height)
	}
}

func TestReplayFollowAutoScrolls(t *testing.T) {
	m := newTestReplayModel()
	m.follow = true
	feedReplayLines(&m, 50)

	if m.scrollOff != m.maxScroll() {
		t.Errorf("scrollOff = %d, want %d (auto-scroll)", m.scrollOff, m.maxScroll())
	}
}

func TestReplayScrollClampAtZero(t *testing.T) {
	m := newTestReplayModel()
	m.scrollOff = 0
	m.follow = false

	m = sendReplayKey(m, "k")
	if m.scrollOff != 0 {
		t.Errorf("scrollOff = %d, want 0 (clamped)", m.scrollOff)
	}
}

func TestReplayScrollClampAtMax(t *testing.T) {
	m := newTestReplayModel()
	feedReplayLines(&m, 50)
	m.scrollOff = m.maxScroll()
	m.follow = false

	m = sendReplayKey(m, "j")
	if m.scrollOff != m.maxScroll() {
		t.Errorf("scrollOff = %d, want %d (clamped)", m.scrollOff, m.maxScroll())
	}
}

func TestFormatSpeedValues(t *testing.T) {
	tests := []struct {
		in   Speed
		want string
	}{
		{0, "instant"},
		{1, "1x"},
		{2, "2x"},
		{10, "10x"},
		{0.5, "0.5x"},
	}
	for _, tt := range tests {
		got := formatSpeed(tt.in)
		if got != tt.want {
			t.Errorf("formatSpeed(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want string
	}{
		{0, "00:00"},
		{30 * time.Second, "00:30"},
		{5*time.Minute + 30*time.Second, "05:30"},
		{1*time.Hour + 5*time.Minute + 30*time.Second, "01:05:30"},
	}
	for _, tt := range tests {
		got := formatDuration(tt.in)
		if got != tt.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestReplayViewDoneBadge(t *testing.T) {
	ring := recv.NewLogRing(100)
	meta := &recv.Metadata{Version: 1, Format: "jsonl"}

	// create a feeder that completes instantly
	dir := t.TempDir()
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	writeMetadata(t, dir, base, base, 0)
	reader, err := NewReader(dir)
	if err != nil {
		t.Fatal(err)
	}
	feeder := NewFeeder(reader, ring, nil, SpeedInstant)
	feeder.Start()
	// wait for done
	for !feeder.Done() {
		time.Sleep(10 * time.Millisecond)
	}

	m := NewReplayModel(feeder, ring, meta, dir, 0)
	m.width = 120
	m.height = 30
	m.startTime = time.Now()

	view := m.View()
	if !strings.Contains(view, "COMPLETE") {
		t.Error("expected COMPLETE badge in view when done")
	}
}
