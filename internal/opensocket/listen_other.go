//go:build !(darwin || linux || freebsd || netbsd || openbsd || dragonfly)

package opensocket

import (
	"errors"
	"net"
)

// listen is not implemented here (Windows among others: it needs
// flock); slk runs without the socket.
func listen(string) (net.Listener, func() error, error) {
	return nil, nil, errors.ErrUnsupported
}
