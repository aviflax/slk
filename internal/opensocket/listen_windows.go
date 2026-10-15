//go:build windows

package opensocket

import (
	"errors"
	"net"
)

// listen is not implemented on Windows; slk runs without the socket
// there.
func listen(string) (net.Listener, error) {
	return nil, errors.ErrUnsupported
}
