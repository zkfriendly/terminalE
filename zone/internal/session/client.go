package session

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

// DaemonArg is the argv[1] used to launch the background daemon process.
const DaemonArg = "__daemon"

// Client is a connection from the TUI to the focus daemon.
type Client struct {
	mu   sync.Mutex
	conn net.Conn
	enc  *json.Encoder
	dec  *json.Decoder
}

// Dial connects to a running daemon. It fails if no daemon is listening.
func Dial() (*Client, error) {
	conn, err := dialDaemon(time.Second)
	if err != nil {
		return nil, err
	}
	return &Client{
		conn: conn,
		enc:  json.NewEncoder(conn),
		dec:  json.NewDecoder(conn),
	}, nil
}

// IsAlive reports whether a daemon is currently listening.
func IsAlive() bool {
	conn, err := dialDaemon(500 * time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func (c *Client) do(cmd Command) (Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var snap Snapshot
	if err := c.enc.Encode(cmd); err != nil {
		return snap, err
	}
	if err := c.dec.Decode(&snap); err != nil {
		return snap, err
	}
	return snap, nil
}

// Status fetches the current snapshot.
func (c *Client) Status() (Snapshot, error) { return c.do(Command{Op: OpStatus}) }

// Toggle pauses/resumes the session.
func (c *Client) Toggle() (Snapshot, error) { return c.do(Command{Op: OpToggle}) }

// Skip skips the current block.
func (c *Client) Skip() (Snapshot, error) { return c.do(Command{Op: OpSkip}) }

// Track toggles an ambient layer by index.
func (c *Client) Track(i int) (Snapshot, error) { return c.do(Command{Op: OpTrack, Index: i}) }

// SetVolume sets playback volume (0..1).
func (c *Client) SetVolume(v float64) (Snapshot, error) {
	return c.do(Command{Op: OpVolume, Volume: v})
}

// Preview plays the start and end chimes so the user knows what to listen for.
func (c *Client) Preview() (Snapshot, error) { return c.do(Command{Op: OpPreview}) }

// SetTask switches the current task (0 = no task / just focus).
func (c *Client) SetTask(taskID int64) (Snapshot, error) {
	return c.do(Command{Op: OpSetTask, TaskID: taskID})
}

// Adjust scrubs the current block by delta seconds (+rewind, −skip ahead).
func (c *Client) Adjust(delta int) (Snapshot, error) {
	return c.do(Command{Op: OpAdjust, Delta: delta})
}

// End ends the session and stops the daemon.
func (c *Client) End() (Snapshot, error) { return c.do(Command{Op: OpEnd}) }

// Close closes the client connection (does not stop the daemon).
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return nil
	}
	err := c.conn.Close()
	c.conn = nil
	return err
}

// EnsureDaemon makes sure a daemon is running for sessionID, spawning a detached
// background process if necessary, and returns a connected client.
func EnsureDaemon(sessionID int64) (*Client, error) {
	if IsAlive() {
		return Dial()
	}
	if err := spawnDaemon(sessionID); err != nil {
		return nil, err
	}
	// Wait for the daemon to come up.
	for i := 0; i < 40; i++ {
		if IsAlive() {
			return Dial()
		}
		time.Sleep(50 * time.Millisecond)
	}
	return nil, fmt.Errorf("focus daemon did not start")
}

// ResumeDaemon resumes a previously-ended session. A finished daemon may still be
// lingering on the socket (it sticks around for a grace period after a session
// ends); this asks it to exit and waits for it to release the socket, then starts
// a fresh daemon for sessionID that restores its persisted runtime.
func ResumeDaemon(sessionID int64) (*Client, error) {
	if IsAlive() {
		if c, err := Dial(); err == nil {
			_, _ = c.End() // a finished daemon just exits; nothing left to record
			c.Close()
		}
		for i := 0; i < 80 && IsAlive(); i++ {
			time.Sleep(50 * time.Millisecond)
		}
	}
	return EnsureDaemon(sessionID)
}

// spawnDaemon launches the daemon as a detached background process.
func spawnDaemon(sessionID int64) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}

	var logFile *os.File
	if logPath, err := LogPath(); err == nil {
		logFile, _ = os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	}

	cmd := exec.Command(exe, DaemonArg, strconv.FormatInt(sessionID, 10))
	cmd.Stdin = nil
	if logFile != nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}
	// Detach into its own session/process group so it survives the terminal closing.
	setDetached(cmd)

	if err := cmd.Start(); err != nil {
		if logFile != nil {
			logFile.Close()
		}
		return err
	}
	// The parent doesn't wait; release our handle so the child is reparented to init.
	_ = cmd.Process.Release()
	return nil
}
