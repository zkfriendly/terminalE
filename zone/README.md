# zone

A local-first terminal time tracker with a built-in pomodoro **focus zone**.

`zone` lets you organize work into projects and tasks and track time against
them, all stored in a single local SQLite file. But it's more than a tracker:
it's meant to be the center of your working zone. Drop into a full-screen focus
session (the classic 50 minutes on / 10 minutes off over 4 hours), with ambient
sound and gentle chimes at each transition, like a "study with me" stream that
lives in your terminal.

## Features

- Projects and tasks, managed entirely from the keyboard.
- Two ways to track time:
  - **Focus zone** — a full-screen pomodoro session with a big countdown,
    cycle progress, ambient audio, and transition chimes.
  - **Standalone tracking** — start/stop a stopwatch on any task.
- Every session opens with a short **prepare** block (3 min by default): a calm
  welcome screen that nudges you to grab water, breathe, and settle in, and
  previews the chimes so you know how a session starts and ends. Configurable —
  set to 0 to skip.
- Mixable **ambient sound layers** you can toggle on/off and overlap during a
  session: 15 Hz and 45 Hz focus beats, plus an optional loop of your own audio
  files.
- **Background focus daemon**: a focus session keeps running (and playing sound)
  even after you close the terminal. Reopen zone and it picks up right where you
  left off.
- Live **stats**: today's focus time, last 7 days, current streak,
  per-project and per-task breakdowns, and recent session history.
- Local-first: everything is in one SQLite database. No accounts, no network.

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

## Keys

### Dashboard

| Key            | Action                                   |
| -------------- | ---------------------------------------- |
| `↑`/`↓`, `j/k` | Move selection                           |
| `tab`, `h/l`   | Switch between Projects and Tasks panes  |
| `enter` / `f`  | Enter the focus zone for the task        |
| `t`            | Start/stop standalone tracking on a task |
| `n`            | New project / task (in focused pane)     |
| `e`            | Rename selected project / task           |
| `x`            | Toggle task done                         |
| `d`            | Archive selected project / task          |
| `s`            | Open stats                               |
| `q`            | Quit                                     |

### Focus zone

| Key       | Action                                            |
| --------- | ------------------------------------------------- |
| `space`   | Pause / resume                                    |
| `p`       | Preview the start + end chimes                     |
| `s`       | Skip the current block (during prepare: start now) |
| `1`-`9`   | Toggle ambient sound layers (overlap allowed)     |
| `+` / `-` | Volume up / down                                  |
| `b` / `q` | Background: leave the session running, go to dashboard |
| `esc`     | End the session (stops the background daemon)     |

Closing the terminal (or `ctrl+c`) also just backgrounds the session — it keeps
running. Only `esc` ends it.

### Stats

| Key   | Action  |
| ----- | ------- |
| `r`   | Refresh |
| `esc` | Back    |

## Background focus daemon

When you start a focus session, zone launches a small detached background process
(`zone __daemon <id>`) that owns the running timer and audio. This means:

- You can close the terminal (or quit the UI) and the session keeps running and
  playing sound.
- Reopen zone any time and it auto-reconnects, showing a "focus session running"
  banner on the dashboard and dropping you back into the live session (press
  `enter` to resume the full-screen view).
- The daemon persists its state to the database every second, so it can recover
  even across reboots.

Only one focus session runs at a time. The daemon talks to the UI over a Unix
socket (`zoned.sock`) in the config dir and logs to `zoned.log`.

## Configuration

`config.json` is created with defaults on first run. Edit it to change the
session shape and audio:

```json
{
  "prepare_minutes": 3,
  "work_minutes": 50,
  "break_minutes": 10,
  "total_minutes": 240,
  "ambient_mode": "noise",
  "ambient_folder": "",
  "volume": 0.6,
  "chimes_enabled": true,
  "layers": {
    "beat15": false,
    "beat45": false
  }
}
```

- `prepare_minutes`: a one-off settle-in block before the first work block. It is
  not counted as work time. Set to `0` to start working immediately.
- `total_minutes / (work_minutes + break_minutes)` determines the number of
  cycles in a session (50/10 over 240 = 4 cycles); the prepare block is extra.
- `layers`: which ambient sound layers start enabled. Toggle them live with
  number keys during a session; your last selection and volume are saved here.
  The beat layers (15 Hz / 45 Hz) are binaural beats and work best with
  headphones.
- `ambient_folder`: if set, a "playlist" layer becomes available that loops the
  audio files in that folder (`.mp3`, `.wav`, `.flac`, `.ogg`).

## Architecture

```
main.go              entry point: run TUI, or the `__daemon` background process
internal/
  config/            settings (JSON) + session/audio shape
  db/                SQLite open + embedded schema (WAL, foreign keys) + migrations
  store/             repositories: projects, tasks, sessions, entries, stats
  timer/             pure pomodoro state machine (work/break cycles) + tests
  audio/             beep-based ambient layers (noise / beats / folder) + chimes
  session/           focus daemon: server, client, IPC protocol, process spawn
  tui/               Bubble Tea v2 app: dashboard, focus zone, stats views
```

The pomodoro engine in `internal/timer` is a pure, I/O-free state machine. The
focus daemon (`internal/session`) owns one engine plus the audio mixer, ticks it
once per second, persists each finished block as a time entry, and saves its live
state to the database continuously so a session can be resumed. The TUI is a thin
client that renders snapshots from the daemon and sends it commands over a Unix
socket — which is what lets a session outlive the terminal that started it.

## Tech

- [Bubble Tea v2](https://github.com/charmbracelet/bubbletea) + Lip Gloss v2 for the TUI
- [`modernc.org/sqlite`](https://pkg.go.dev/modernc.org/sqlite) — pure-Go SQLite (no CGo)
- [`gopxl/beep`](https://github.com/gopxl/beep) for audio playback

## Tests

```bash
go test ./...
```
