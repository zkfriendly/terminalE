//go:build !windows

package session

import (
	"net"
	"os"
	"time"
)

// The daemon and TUI talk over a Unix-domain socket on macOS/Linux. See
// transport_windows.go for the named-pipe equivalent.

// dialDaemon connects to the running daemon's endpoint.
func dialDaemon(timeout time.Duration) (net.Conn, error) {
	sock, err := SocketPath()
	if err != nil {
		return nil, err
	}
	return net.DialTimeout("unix", sock, timeout)
}

// listenDaemon clears any stale socket file and starts listening.
func listenDaemon() (net.Listener, error) {
	sock, err := SocketPath()
	if err != nil {
		return nil, err
	}
	_ = os.Remove(sock) // clear any stale socket
	return net.Listen("unix", sock)
}

// removeEndpoint deletes the socket file on shutdown.
func removeEndpoint() {
	if sock, err := SocketPath(); err == nil {
		_ = os.Remove(sock)
	}
}
