# zone

A local-first terminal time tracker with a built-in pomodoro **focus zone**.

`zone` lets you organize work into projects and tasks and track time against
them, all stored in a single local SQLite file. But it's more than a tracker:
it's meant to be the center of your working zone. Drop into a full-screen focus
session (the classic 50 minutes on / 10 minutes off over 4 hours) with gentle
chimes at each transition.

## Features

- Projects and tasks, managed entirely from the keyboard.
- Two ways to track time:
  - **Focus zone** — a full-screen pomodoro session with a big countdown,
    cycle progress, and transition chimes.
  - **Standalone tracking** — start/stop a stopwatch on any task.
- Focus sessions are **general**, not tied to one task: start a session and
  switch which task you're working on at any time (press `t`). Time is attributed
  to whichever task is current — switching mid-block splits it correctly. You can
  also run with no task at all ("just focus").
- Every session opens with a short **prepare** block (3 min by default): a calm
  welcome screen that nudges you to grab water, breathe, and settle in.
  Configurable — set to 0 to skip.
- **Background focus daemon**: a focus session keeps running even after you close
  the terminal. Reopen zone and it picks up right where you left off.
- Live **stats**: today's focus time, last 7 days, current streak,
  per-project and per-task breakdowns, and recent session history.
- Local-first: projects, timers, and notes are stored in one SQLite database.
  Optional AI features use Codex by default, or a configurable local model server.

## Install / Run

Requires Go 1.25+.

```bash
cd zone
go run .
# or build a binary:
go build -o zone .
./zone
```

Data and config live in your user config directory:

- macOS: `~/Library/Application Support/zone/`
- Linux: `~/.config/zone/`

Files: `zone.db` (database) and `config.json` (settings).

AI note features default to the installed **Codex CLI**, using its existing
sign-in (`codex login`). To use a local model instead, open **Config → AI provider**
and press **enter** or **space** to select `local`. Turn **AI notes** off to disable
AI requests entirely. Tracking and note editing work without an AI provider.

On macOS that directory is `~/Library/Application Support/zone/`; on Linux it is
`~/.config/zone/`. Open **Config** from the top nav to edit settings in
the app — changes are saved to `config.json` immediately.

## Keys

zone uses an **OS-style shell** outside the focus zone: a top tab bar lists
Work, Stats, History, and Config; the main area shows the active screen. A status
bar at the bottom shows key hints; the title row shows the clock and session state.

| Key            | Action                                            |
| -------------- | ------------------------------------------------- |
| `tab`          | Switch focus between the tab bar and main content |
| `←`/`→`, `h/l` | Move between tabs (when the tab bar is focused)   |
| `↑`/`↓`, `j/k` | Move selection in lists (also works on tabs)      |
| `enter`        | Open the selected tab                             |
| `esc`          | Back one level (close menu → leave page → quit)   |

### Work (projects & tasks)

| Key                   | Action                                                                                 |
| --------------------- | -------------------------------------------------------------------------------------- |
| `←`/`→`, `h/l`        | Move between nested task levels                                                      |
| `↑`/`↓`, `j/k`        | Move selection in the active column                                                  |
| `enter`, `f`, `space` | Start focus on selected task, or resume a running session                            |
| `R`                   | Resume the session you ended early (when the banner is shown)                          |
| `n`                   | New task at the current level (sibling of the selection)                             |
| `N`                   | New child task under the selected task                                               |
| `e`                   | Rename selected project or task                                                        |
| `d`                   | Archive selected project or task                                                       |
| `t`                   | Start/stop standalone time tracking on the selected task                               |
| `x`                   | Mark task done / reopen                                                                |
| `r`                   | Refresh projects, tasks, and session state                                             |

### Focus zone

| Key     | Action                                                           |
| ------- | ---------------------------------------------------------------- |
| `space` | Pause / resume                                                   |
| `t`     | Switch the current task (or "just focus")                        |
| `n`     | Take session notes (vim-style editor, `:wq` to save)             |
| `s`     | Skip the current block (during prepare: start now)               |
| `b`     | Background: leave the session running, go to dashboard           |
| `E`     | End the session early (stops the daemon; confirm with `y`)       |
| `esc`   | Background the session **and quit** zone (works from any screen) |

Closing the terminal (or `ctrl+c`) also just backgrounds the session — it keeps
running.

`esc` is the universal "background & quit": from anywhere except an overlay or
text editor (session notes, task picker, or the new/rename inputs) it leaves any
running focus session alive in the background and exits zone in one keystroke.
Use `b` during a focus session to background without quitting. Press `E` to end
a session early (with confirmation); the dashboard will offer to resume it later.

**Session notes:** press `n` during a focus session to open the note browser.
Press **`/`** or **`ctrl+f`** there (or on the **Notes** tab) to search all notes with an
LLM summary and follow-up chat. Pick an earlier note to edit or choose **+ new note**. When you save a note,
zone asks the selected AI provider for a short **title** and **emoji** label.
Codex is the default; the local provider supports LM Studio's OpenAI-compatible
API and Ollama. If labeling fails, zone shows the
error and leaves the note **unlabeled** (empty title/emoji) so you can tell which
notes still need labels. Editing is vim-style: insert,
`esc` for normal, `i`/`a`/`o` to insert, `:w` to save, `:wq` or `ZZ` to save and
return to focus, `:q` to discard and close. Changes are not saved until you run
`:w`, `:wq`, or `ZZ` — `esc` back to the browser or switching notes discards
unsaved edits. In the browser, `d` deletes the selected note (confirm with `y`);
deletion is permanent. `esc` in the browser closes notes. View notes later from
**History →** (`n` on a session).

If you end a session early, the Work page shows a banner — press `R` to resume
it, or start fresh with `f` / `enter`.

### Stats & History

Browse from the top nav. Press `r` to refresh. `esc` returns to Work.

### Notes

Browse every session note from the top nav (**4:Notes**). Press **`/`** or **`ctrl+f`** to **search** your notes:
type a natural-language question (e.g. `what about whir do I know`). Search is **LLM-assisted**:
the selected model expands your question into keywords and a hypothetical matching note (HyDE),
merges keyword hits with reciprocal rank fusion, then **reranks** candidates by relevance before
summarizing. Requires **AI notes** enabled in Config (same provider used for note labels and task extraction). **`↑`/`↓`** selects a matching note (from the chat pane when the follow-up
box is empty, or from the results pane after **`tab`**). Press **`enter`** or **`o`**
to open the highlighted note for editing.

While editing a note, search **inside** that note with vim-style **`/`** / **`?`**
or **`ctrl+f`**, then **`n`** / **`N`** for next/previous match.

| Key                    | Action                                      |
| ---------------------- | ------------------------------------------- |
| `↑`/`↓`, `j/k`         | Move in the note list                       |
| `enter`                | Open selected note                          |
| `/` or `ctrl+f`        | Search notes (LLM summary + chat)           |
| `↑`/`↓` or `tab`       | Select a search result                      |
| `enter` or `o`         | Open highlighted search result              |
| `/` or `?` or `ctrl+f` | Find text inside an open note (normal mode) |
| `n` / `N`              | Next / previous in-note match               |
| `t`                    | View extracted tasks (when available)       |
| `d`                    | Delete note                                 |
| `r`                    | Refresh                                     |

Each session shows both **focus** time (active work, excluding pauses and
breaks) and **wall** time (total clock time from start to end). Useful when you
pause/resume a lot and want to see how long a session really took.

### Session history

A scrollable list of every focus session (date, task or "general focus",
focus time, wall time, and status).

| Key       | Action             |
| --------- | ------------------ |
| `↑` / `↓` | Select session     |
| `n`       | View session notes |
| `g` / `G` | Jump top/bottom    |
| `r`       | Refresh            |

### Config

| Key       | Action                          |
| --------- | ------------------------------- |
| `↑` / `↓` | Select a setting                |
| `enter`   | Edit numbers and text fields    |
| `space`   | Toggle booleans / switch AI provider |

## Background focus daemon

When you start a focus session, zone launches a small detached background process
(`zone __daemon <id>`) that owns the running timer and audio. This means:

- You can close the terminal (or quit the UI) and the session keeps running and
  playing sound.
- Reopen zone any time and it auto-reconnects, showing a "focus session running"
  banner on the Work page — press `enter` or `f` to return to the live session.
- The daemon persists its state to the database every second, so it can recover
  even across reboots.

Only one focus session runs at a time. The daemon talks to the UI over a Unix
socket (`zoned.sock`) in the config dir and logs to `zoned.log`.

## Configuration

`config.json` is created with defaults on first run. Edit it to change the
session shape:

```json
{
  "prepare_minutes": 3,
  "work_minutes": 50,
  "break_minutes": 10,
  "total_minutes": 240,
  "llm_enabled": true,
  "llm_provider": "codex",
  "codex_command": "codex",
  "codex_model": "",
  "lm_studio_url": "http://127.0.0.1:11434",
  "lm_studio_model": ""
}
```

- `llm_enabled`: enables note labels, actionable task detection/extraction, and
  note search/chat. Old `lm_studio_enabled` settings are still read, including
  `false`; `llm_enabled` takes precedence if both keys exist.
- `llm_provider`: `codex` (default) or `local`. Switching takes effect for new
  requests immediately and preserves each provider's settings. Existing config
  files without this key also use Codex. An explicit provider choice is saved.
- `codex_command`: executable name on `PATH` or a path to the Codex binary.
  This is not a shell command; do not append arguments.
- `codex_model`: optional model override. Empty uses the Codex CLI default.
  Zone reuses Codex authentication but ignores its user config for these requests,
  so coding-specific settings do not affect note processing. Clear this field in
  Config to return to automatic model selection.
- `lm_studio_url`: local server base URL, defaulting to Ollama's port `11434`.
  For LM Studio, use `http://127.0.0.1:1234`. The local backend supports both
  OpenAI-compatible and native Ollama endpoints.
- `lm_studio_model`: local model name; leave empty to auto-detect a chat model.

With Codex selected, note text and relevant search context are sent through your
Codex account. Zone uses the documented
[`codex exec` non-interactive interface](https://learn.chatgpt.com/docs/non-interactive-mode):
requests run in temporary directories, use read-only sandboxing, disable shell,
plugins, apps, hooks, and subagents, and do not persist Codex session history.
Each request has a two-minute timeout; at most two Codex processes run at once.
Missing Codex, sign-in errors, or invalid responses appear in Zone's existing
error UI. Zone never automatically switches between cloud and local providers.

- `prepare_minutes`: a one-off settle-in block before the first work block. It is
  not counted as work time. Set to `0` to start working immediately.
- `total_minutes / (work_minutes + break_minutes)` determines the number of
  cycles in a session (50/10 over 240 = 4 cycles); the prepare block is extra.

## Architecture

```
main.go              entry point: run TUI, or the `__daemon` background process
internal/
  config/            settings (JSON) + session shape
  llm/               shared note prompts/parsers + Codex and local server providers
  db/                SQLite open + embedded schema (WAL, foreign keys) + migrations
  store/             repositories: projects, tasks, sessions, entries, stats
  timer/             pure pomodoro state machine (work/break cycles) + tests
  audio/             transition chimes (optional ambient layers, off by default)
  session/           focus daemon: server, client, IPC protocol, process spawn
  tui/               Bubble Tea v2 app: dashboard, focus zone, stats views
```

The pomodoro engine in `internal/timer` is a pure, I/O-free state machine. The
focus daemon (`internal/session`) owns one engine plus optional chimes, ticks it
once per second, persists each finished block as a time entry, and saves its live
state to the database continuously so a session can be resumed. The TUI is a thin
client that renders snapshots from the daemon and sends it commands over a Unix
socket — which is what lets a session outlive the terminal that started it.

All note AI features use `llm.Client`, which depends on the small `llm.Provider`
interface. Backend selection is centralized in `llm.NewClient`; the built-in
default lives in `config.DefaultLLMProvider`. Adding another provider requires a
transport implementation and a factory/config option, without changing note
prompts, parsing, or TUI feature code. Commands capture their provider settings
before running, so editing Config does not change an in-flight search halfway
through its expansion, reranking, and answer steps.

## Tech

- [Bubble Tea v2](https://github.com/charmbracelet/bubbletea) + Lip Gloss v2 for the TUI
- [`modernc.org/sqlite`](https://pkg.go.dev/modernc.org/sqlite) — pure-Go SQLite (no CGo)
- [`gopxl/beep`](https://github.com/gopxl/beep) for audio playback

## Tests

```bash
go test ./...
```

The normal suite uses a fake CLI process and local HTTP test servers, with no
Codex account or network access required. To smoke-test the installed Codex CLI
with a synthetic note:

```bash
ZONE_TEST_CODEX=1 go test ./internal/llm -run '^TestCodexLive$' -v -count=1
```
