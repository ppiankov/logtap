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
- `Esc` — clear search/filter and restore all lines

### Status bar badges

| Badge | Meaning |
|-------|---------|
| `[1/42] /error` | Search mode — match 1 of 42 |
| `HIDE: /healthz [8421 lines]` | Hide mode — 8421 lines remaining |
| `GREP: /trace-abc [3 lines]` | Grep mode — 3 lines matching |
| `FOLLOW` | Auto-scrolling to new lines |

## Label filter

Press `l` to enter label filter mode.

| Input | Action |
|-------|--------|
| `l` then `container=api` | Show only lines where label `container` equals `api` |
| `l` then `Enter` (empty) | Clear label filter |
| `Esc` | Cancel and clear filter |

Label filter and search/grep can be combined — label filter applies first.

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
