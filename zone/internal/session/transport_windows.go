//go:build windows

package session

import (
	"hash/fnv"
	"net"
	"strconv"
	"time"

	"github.com/Microsoft/go-winio"
	"github.com/zkfriendly/zone/internal/config"
)

// On Windows there are no Unix-domain sockets (net.Listen("unix", …) fails to
// bind), so the daemon and TUI talk over a named pipe instead. The pipe name is
// derived from the user's config directory so distinct users don't collide.

func pipePath() string {
	name := "zone-daemon"
	if dir, err := config.Dir(); err == nil {
		h := fnv.New32a()
		_, _ = h.Write([]byte(dir))
		name = "zone-" + strconv.FormatUint(uint64(h.Sum32()), 16)
	}
	return `\\.\pipe\` + name
}

// dialDaemon connects to the running daemon's named pipe.
func dialDaemon(timeout time.Duration) (net.Conn, error) {
	t := timeout
	return winio.DialPipe(pipePath(), &t)
}

// listenDaemon starts listening on the named pipe. winio creates the pipe with
// FILE_FLAG_FIRST_PIPE_INSTANCE, so this fails if a daemon already owns it.
func listenDaemon() (net.Listener, error) {
	return winio.ListenPipe(pipePath(), nil)
}

// removeEndpoint is a no-op on Windows: the named pipe is released automatically
// when the listener closes.
func removeEndpoint() {}
