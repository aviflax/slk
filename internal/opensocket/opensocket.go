// Package opensocket lets other programs hand a Slack permalink to a
// running slk over a unix socket.
//
// The protocol is one request per connection: the client writes a URL
// followed by a newline, the server writes back one line, "ok" or
// "error: <reason>", and closes. The server rejects anything
// slackurl.Parse does not accept before calling its Handler, and reads
// at most MaxRequest bytes.
//
// Listen is the server end (started by the TUI), Send the client end
// (the `slk open` subcommand). Where flock is unavailable (Windows)
// Listen returns errors.ErrUnsupported and Send reports ErrNotRunning.
package opensocket

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gammons/slk/internal/slackurl"
)

// DirName is the private (0700) subdirectory of slk's data directory
// that holds the socket and its lock file.
const DirName = "run"

// FileName is the socket's name inside DirName.
const FileName = "slk.sock"

// MaxRequest caps the bytes the server reads from one connection,
// newline included. A Slack permalink is well under 200 bytes.
const MaxRequest = 4096

// ExchangeTimeout bounds a whole connection on either end: reading the
// request, running the Handler and writing the reply. A Handler that
// waits on something should give up before this.
const ExchangeTimeout = 10 * time.Second

// ErrInUse means another process is already listening on the socket.
// Listen leaves that socket alone.
var ErrInUse = errors.New("another slk is already listening")

// ErrNotRunning means nothing accepted the connection: no slk is
// running, or it is running without a listener.
var ErrNotRunning = errors.New("slk is not running")

// Rejected is the reason a running slk gave for not opening a URL.
type Rejected struct{ Reason string }

func (r *Rejected) Error() string { return r.Reason }

// Handler opens a validated permalink. A nil error replies "ok"; an
// error replies "error: " plus its message.
type Handler func(url string) error

// Path returns the socket path for dataDir: dataDir/run/slk.sock.
func Path(dataDir string) string { return filepath.Join(dataDir, DirName, FileName) }

// Server is a running listener. Close stops it and removes the socket.
type Server struct {
	ln      net.Listener
	stop    func() error
	handle  Handler
	wg      sync.WaitGroup
	closeMu sync.Once
}

// Listen takes the socket at path and serves it until Close. The
// socket's directory is created or narrowed to 0700 and must belong to
// this user. Listen returns ErrInUse when another process answers on
// path, and an error without touching the file when something other
// than a socket is there. A socket nobody answers on is left over from
// a crashed instance and is replaced; instances starting together
// serialize on a lock file beside the socket, so only one replaces it.
func Listen(path string, handle Handler) (*Server, error) {
	ln, stop, err := listen(path)
	if err != nil {
		return nil, err
	}
	s := &Server{ln: ln, stop: stop, handle: handle}
	s.wg.Add(1)
	go s.serve()
	return s, nil
}

// Close stops accepting, removes the socket file if it is still the one
// this Server bound (never a later instance's), and waits for the
// accept loop to exit. Connections already accepted finish on their own
// deadline.
func (s *Server) Close() error {
	var err error
	s.closeMu.Do(func() {
		err = s.stop()
		s.wg.Wait()
	})
	return err
}

func (s *Server) serve() {
	defer s.wg.Done()
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			// A transient accept failure (EMFILE and the like); back
			// off briefly rather than spin.
			time.Sleep(50 * time.Millisecond)
			continue
		}
		go s.serveConn(conn)
	}
}

func (s *Server) serveConn(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(ExchangeTimeout))
	reply := "ok"
	if err := s.request(conn); err != nil {
		reply = "error: " + oneLine(err.Error())
	}
	_, _ = io.WriteString(conn, reply+"\n")
}

func (s *Server) request(conn net.Conn) error {
	url, err := readRequest(conn)
	if err != nil {
		return err
	}
	if _, ok := slackurl.Parse(url); !ok {
		return errors.New("not a Slack message permalink")
	}
	return s.handle(url)
}

// readRequest reads one line of at most MaxRequest bytes. A client that
// closes its write side without a newline still gets its line read.
func readRequest(r io.Reader) (string, error) {
	br := bufio.NewReader(io.LimitReader(r, MaxRequest))
	line, err := br.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("reading request: %w", err)
	}
	if !strings.HasSuffix(line, "\n") && len(line) >= MaxRequest {
		return "", fmt.Errorf("request longer than %d bytes", MaxRequest)
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return "", errors.New("empty request")
	}
	return line, nil
}

// oneLine keeps a reply to one line whatever the error text holds.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// Send hands url to the slk listening on path. It returns nil when slk
// replied "ok", ErrNotRunning (wrapped) when nothing accepted the
// connection, and *Rejected when slk declined the URL.
func Send(path, url string) error {
	conn, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNotRunning, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(ExchangeTimeout))
	if _, err := io.WriteString(conn, oneLine(url)+"\n"); err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	line, err := bufio.NewReader(io.LimitReader(conn, MaxRequest)).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("reading reply: %w", err)
	}
	line = strings.TrimSpace(line)
	switch {
	case line == "ok":
		return nil
	case strings.HasPrefix(line, "error: "):
		return &Rejected{Reason: strings.TrimPrefix(line, "error: ")}
	case line == "":
		return errors.New("slk closed the connection without replying")
	default:
		return fmt.Errorf("unexpected reply %q", line)
	}
}
