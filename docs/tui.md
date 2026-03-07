# TUI Keybindings

The logtap TUI is available in two modes: live receiver (`logtap recv`) and replay (`logtap open`). Both share the same keybindings.

## Navigation

| Key | Action |
|-----|--------|
| `j` / `Down` | Scroll down one line |
| `k` / `Up` | Scroll up one line |
| `d` | Scroll down half page |
| `u` | Scroll up half page |
| `G` | Jump to bottom (enables follow) |
| `gg` | Jump to top |
| `f` | Toggle follow mode (auto-scroll on new lines) |

## Search

Press `/` to enter search mode, then type a pattern and press `Enter`.

| Input | Mode | Behavior |
|-------|------|----------|
| `/pattern` | Search | Highlight matching lines, navigate with `n`/`N` |
| `/!pattern` | Hide | Remove matching lines from view |
| `/=pattern` | Grep | Show **only** matching lines |

- Patterns are Go regular expressions
- Matches against both the log message and label values
- `n` — jump to next match (search mode only)
- `N` — jump to previous match (search mode only)
- `Esc` — unwind filters one at a time (clear highlight first, then pop last filter)

### Filter stacking

Hide (`/!`) and grep (`/=`) filters **stack** — each new filter narrows the previous result:

```
/!linkerd        → hides linkerd lines
/!healthz        → also hides healthz lines
/=error          → from remainder, show only error lines
/trace-abc       → highlight trace-abc within the filtered view
```

- `Esc` pops one filter at a time (highlight first, then stack top-down)
- Highlight (`/pattern`) is separate from the stack — it doesn't filter, just marks matches
- Label filter (`l`) is independent — applied before the search stack

### Status bar badges

| Badge | Meaning |
|-------|---------|
| `[1/42] /error` | Search mode — match 1 of 42 |
| `HIDE: /healthz` | Hide filter in stack |
| `GREP: /trace-abc` | Grep filter in stack |
| `[42 lines]` | Lines remaining after all filters |
| `FOLLOW` | Auto-scrolling to new lines |

## Label filter

Press `l` to enter label filter mode.

| Input | Action |
|-------|--------|
| `l` then `container=api` | Show only lines where label `container` equals `api` |
| `l` then `Enter` (empty) | Clear label filter |
| `Esc` | Cancel and clear filter |

Label filter and search/grep can be combined — label filter applies first.

## Time jump

Press `t` to jump to a specific timestamp.

| Input | Action |
|-------|--------|
| `t` then `14:32` | Jump to first line at 14:32 |
| `t` then `14:32:05` | Jump to first line at 14:32:05 |
| `t` then `2026-03-05T14:32` | Jump to specific date+time |
| `Esc` | Cancel time jump |

Partial matches work — the input is matched against the formatted timestamp string.

## Export

Press `w` to export the current filtered view as a new capture directory.

| Input | Action |
|-------|--------|
| `w` | Enter export mode — type output directory path |
| `Enter` | Write filtered lines to the specified directory |
| `Esc` | Cancel export |

The exported directory is a valid logtap capture (metadata.json + index.jsonl + data) that can be opened with `logtap open`, inspected, or shared.

## General

| Key | Action |
|-----|--------|
| `?` | Toggle help overlay |
| `q` | Quit |
| `Ctrl+C` | Quit |

## Stats pane

The top section shows live stats (updated every second):

| Field | Description |
|-------|-------------|
| Connections | Active HTTP connections to the receiver |
| Logs/sec | Incoming log rate |
| Bytes/sec | Write throughput |
| Disk used | Current / max disk usage |
| Dropped | Lines dropped due to backpressure (red if > 0) |
| Redact | Redaction status — shows warning if `--redact` is not enabled |
| Top talkers | Top 5 label values by log volume |
