// Command zone is a local-first terminal time tracker with a built-in pomodoro
// focus "zone": projects and tasks backed by SQLite, plus a full-screen 50/10
// over 4h focus session with ambient audio, transition chimes, and live stats.
package main

import (
	"fmt"
	"os"
	"strconv"

	tea "charm.land/bubbletea/v2"
	"github.com/zkfriendly/zone/internal/config"
	"github.com/zkfriendly/zone/internal/db"
	"github.com/zkfriendly/zone/internal/session"
	"github.com/zkfriendly/zone/internal/store"
	"github.com/zkfriendly/zone/internal/tui"
)

func main() {
	// Background focus daemon: `zone __daemon <session-id>`.
	if len(os.Args) >= 3 && os.Args[1] == session.DaemonArg {
		id, err := strconv.ParseInt(os.Args[2], 10, 64)
		if err != nil {
			fmt.Fprintln(os.Stderr, "zone daemon: bad session id:", err)
			os.Exit(1)
		}
		if err := session.RunDaemon(id); err != nil {
			fmt.Fprintln(os.Stderr, "zone daemon:", err)
			os.Exit(1)
		}
		return
	}

	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "zone:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	dbPath, err := db.DefaultPath()
	if err != nil {
		return fmt.Errorf("resolve db path: %w", err)
	}
	database, err := db.Open(dbPath)
	if err != nil {
		return err
	}
	defer database.Close()

	st := store.New(database)

	app := tui.NewApp(st, cfg)
	p := tea.NewProgram(app)
	_, err = p.Run()
	return err
}
