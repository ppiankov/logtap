package archive

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Participant represents a service in the sequence diagram.
type Participant struct {
	Name  string `json:"name"`
	Index int    `json:"index"`
}

// Interaction represents an arrow between two participants.
type Interaction struct {
	From       string  `json:"from"`
	To         string  `json:"to"`
	LagSeconds float64 `json:"lag_seconds"`
	Pattern    string  `json:"pattern"`
	Confidence float64 `json:"confidence"`
	Label      string  `json:"label"`
}

// SequenceDiagram holds participants and interactions for ASCII rendering.
type SequenceDiagram struct {
	Participants []Participant `json:"participants"`
	Interactions []Interaction `json:"interactions"`
}

// BuildSequence converts correlations into a renderable sequence diagram.
func BuildSequence(correlations []Correlation) *SequenceDiagram {
	if len(correlations) == 0 {
		return &SequenceDiagram{}
	}

	// collect unique services, tracking first appearance as source
	sourceOrder := make(map[string]int) // service → first index as source
	allServices := make(map[string]bool)
	for i, c := range correlations {
		allServices[c.Source] = true
		allServices[c.Target] = true
		if _, ok := sourceOrder[c.Source]; !ok {
			sourceOrder[c.Source] = i
		}
	}

	// sort: sources first (by first appearance), then targets-only alphabetically
	names := make([]string, 0, len(allServices))
	for name := range allServices {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		oi, isSrc := sourceOrder[names[i]]
		oj, jSrc := sourceOrder[names[j]]
		if isSrc && !jSrc {
			return true
		}
		if !isSrc && jSrc {
			return false
		}
		if isSrc && jSrc {
			return oi < oj
		}
		return names[i] < names[j]
	})

	// build participants
	indexMap := make(map[string]int, len(names))
	participants := make([]Participant, len(names))
	for i, name := range names {
		participants[i] = Participant{Name: name, Index: i}
		indexMap[name] = i
	}

	// build interactions sorted by lag ascending
	sorted := make([]Correlation, len(correlations))
	copy(sorted, correlations)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].LagSeconds < sorted[j].LagSeconds
	})

	interactions := make([]Interaction, len(sorted))
	for i, c := range sorted {
		label := fmt.Sprintf("%s +%.0fs (%.0f%%)", c.Pattern, c.LagSeconds, c.Confidence*100)
		interactions[i] = Interaction{
			From:       c.Source,
			To:         c.Target,
			LagSeconds: c.LagSeconds,
			Pattern:    c.Pattern,
			Confidence: c.Confidence,
			Label:      label,
		}
	}

	return &SequenceDiagram{
		Participants: participants,
		Interactions: interactions,
	}
}

// minColWidth is the minimum column width for ASCII rendering.
const minColWidth = 16

// WriteASCII renders the sequence diagram as ASCII art.
func (s *SequenceDiagram) WriteASCII(w io.Writer) {
	if len(s.Participants) == 0 {
		return
	}

	// compute column widths
	colWidth := make([]int, len(s.Participants))
	for i, p := range s.Participants {
		cw := len(p.Name) + 4
		if cw < minColWidth {
			cw = minColWidth
		}
		colWidth[i] = cw
	}

	indexMap := make(map[string]int, len(s.Participants))
	for _, p := range s.Participants {
		indexMap[p.Name] = p.Index
	}

	// header line: participant names centered in their columns
	writeHeader(w, s.Participants, colWidth)

	// separator line with vertical bars
	writeLifelines(w, colWidth)

	// interactions
	for _, inter := range s.Interactions {
		fromIdx := indexMap[inter.From]
		toIdx := indexMap[inter.To]
		writeInteraction(w, fromIdx, toIdx, inter.Label, colWidth)
	}

	// closing lifelines
	writeLifelines(w, colWidth)
}

func writeHeader(w io.Writer, participants []Participant, colWidth []int) {
	var sb strings.Builder
	for i, p := range participants {
		name := p.Name
		pad := colWidth[i] - len(name)
		left := pad / 2
		right := pad - left
		sb.WriteString(strings.Repeat(" ", left))
		sb.WriteString(name)
		sb.WriteString(strings.Repeat(" ", right))
	}
	_, _ = fmt.Fprintln(w, strings.TrimRight(sb.String(), " "))
}

func writeLifelines(w io.Writer, colWidth []int) {
	var sb strings.Builder
	for i, cw := range colWidth {
		pos := cw / 2
		if i == 0 {
			sb.WriteString(strings.Repeat(" ", pos))
		} else {
			sb.WriteString(strings.Repeat(" ", pos))
		}
		sb.WriteString("|")
		remaining := cw - pos - 1
		if i < len(colWidth)-1 {
			sb.WriteString(strings.Repeat(" ", remaining))
		}
	}
	_, _ = fmt.Fprintln(w, strings.TrimRight(sb.String(), " "))
}

func writeInteraction(w io.Writer, fromIdx, toIdx int, label string, colWidth []int) {
	if fromIdx == toIdx {
		return
	}

	n := len(colWidth)
	line := make([]byte, 0, 200)

	// compute center positions for each column
	centers := make([]int, n)
	pos := 0
	for i, cw := range colWidth {
		centers[i] = pos + cw/2
		pos += cw
	}

	totalWidth := pos
	buf := make([]byte, totalWidth)
	for i := range buf {
		buf[i] = ' '
	}

	// place lifeline bars
	for i := range colWidth {
		if centers[i] < totalWidth {
			buf[centers[i]] = '|'
		}
	}

	// draw arrow between from and to
	leftIdx, rightIdx := fromIdx, toIdx
	leftToRight := true
	if fromIdx > toIdx {
		leftIdx, rightIdx = toIdx, fromIdx
		leftToRight = false
	}

	leftCenter := centers[leftIdx]
	rightCenter := centers[rightIdx]

	// fill arrow body
	for p := leftCenter + 1; p < rightCenter; p++ {
		buf[p] = '-'
	}

	// place short label in the middle of the arrow
	errLabel := "err"
	labelStr := fmt.Sprintf("[%s]", errLabel)
	mid := (leftCenter + rightCenter) / 2
	labelStart := mid - len(labelStr)/2
	if labelStart < leftCenter+1 {
		labelStart = leftCenter + 1
	}
	if labelStart+len(labelStr) <= rightCenter {
		copy(buf[labelStart:], []byte(labelStr))
	}

	// arrow endpoints
	if leftToRight {
		buf[rightCenter] = '>'
	} else {
		buf[leftCenter] = '<'
	}

	line = append(line, buf...)

	// trim trailing spaces and add annotation
	trimmed := strings.TrimRight(string(line), " ")
	_, _ = fmt.Fprintf(w, "%s   %s\n", trimmed, label)
}

// WriteJSON writes the sequence diagram as JSON.
func (s *SequenceDiagram) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(s)
}
