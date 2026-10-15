//go:build darwin || linux || freebsd || netbsd || openbsd || dragonfly

package opensocket

import (
	"errors"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const permalink = "https://myteam.slack.com/archives/C054JFCBN69/p1779284733270139"

// sockPath returns a socket path short enough for sun_path (104 bytes on
// macOS, where t.TempDir() can exceed it), inside a fresh directory
// removed at cleanup.
func sockPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "slksock")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "run", FileName)
}

// recorder is a Handler that records the URLs it was given and answers
// with err.
type recorder struct {
	mu   sync.Mutex
	urls []string
	err  error
}

func (r *recorder) handle(url string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.urls = append(r.urls, url)
	return r.err
}

func (r *recorder) got() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.urls...)
}

func listenT(t *testing.T, path string, h Handler) *Server {
	t.Helper()
	s, err := Listen(path, h)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// rawExchange writes req verbatim and returns the server's reply line.
func rawExchange(t *testing.T, path, req string) string {
	t.Helper()
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := io.WriteString(conn, req); err != nil {
		t.Fatal(err)
	}
	if uc, ok := conn.(*net.UnixConn); ok {
		_ = uc.CloseWrite()
	}
	b, err := io.ReadAll(conn)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRoundTripDeliversPermalink(t *testing.T) {
	path := sockPath(t)
	rec := &recorder{}
	listenT(t, path, rec.handle)

	if err := Send(path, permalink); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got := rec.got(); len(got) != 1 || got[0] != permalink {
		t.Errorf("handler got %q, want [%q]", got, permalink)
	}
}

func TestHandlerErrorIsRejected(t *testing.T) {
	path := sockPath(t)
	rec := &recorder{err: errors.New("workspace myteam is not the active one")}
	listenT(t, path, rec.handle)

	err := Send(path, permalink)
	var rej *Rejected
	if !errors.As(err, &rej) || rej.Reason != "workspace myteam is not the active one" {
		t.Fatalf("Send = %v, want Rejected with the handler's reason", err)
	}
}

func TestInvalidInputNeverReachesHandler(t *testing.T) {
	path := sockPath(t)
	rec := &recorder{}
	listenT(t, path, rec.handle)

	for _, req := range []string{
		"https://github.com/foo/bar\n",
		"http://myteam.slack.com/archives/C054JFCBN69/p1779284733270139\n",
		"\n",
		"",
		strings.Repeat("a", MaxRequest+10) + "\n",
		permalink, // no newline, but the client closed its side: accepted
	} {
		reply := rawExchange(t, path, req)
		if req == permalink {
			if reply != "ok\n" {
				t.Errorf("unterminated permalink: reply %q, want ok", reply)
			}
			continue
		}
		if !strings.HasPrefix(reply, "error: ") {
			t.Errorf("request %.40q: reply %q, want an error", req, reply)
		}
	}
	if got := rec.got(); len(got) != 1 {
		t.Errorf("handler called with %q, want only the valid permalink", got)
	}
}

func TestPermissions(t *testing.T) {
	path := sockPath(t)
	listenT(t, path, (&recorder{}).handle)

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("socket mode %o, want 600", perm)
	}
	di, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if perm := di.Mode().Perm(); perm != 0o700 {
		t.Errorf("created directory mode %o, want 700", perm)
	}
}

func TestCloseRemovesSocket(t *testing.T) {
	path := sockPath(t)
	s, err := Listen(path, (&recorder{}).handle)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("socket still present after Close: %v", err)
	}
	if err := Send(path, permalink); !errors.Is(err, ErrNotRunning) {
		t.Errorf("Send after Close = %v, want ErrNotRunning", err)
	}
}

func TestStaleSocketIsReplaced(t *testing.T) {
	path := sockPath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	// A socket file nobody listens on, as a crashed instance leaves.
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	_ = ln.Close()
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("stale socket not left behind: %v", err)
	}

	rec := &recorder{}
	listenT(t, path, rec.handle)
	if err := Send(path, permalink); err != nil {
		t.Fatalf("Send to replacement listener: %v", err)
	}
}

func TestLiveSocketIsNotTaken(t *testing.T) {
	path := sockPath(t)
	first := &recorder{}
	listenT(t, path, first.handle)

	if s, err := Listen(path, (&recorder{}).handle); !errors.Is(err, ErrInUse) {
		if s != nil {
			_ = s.Close()
		}
		t.Fatalf("second Listen = %v, want ErrInUse", err)
	}
	if err := Send(path, permalink); err != nil {
		t.Fatalf("first listener stopped answering: %v", err)
	}
	if len(first.got()) != 1 {
		t.Errorf("first listener got %q", first.got())
	}
}

func TestRegularFileIsNotRemoved(t *testing.T) {
	path := sockPath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	if s, err := Listen(path, (&recorder{}).handle); err == nil {
		_ = s.Close()
		t.Fatal("Listen replaced a regular file")
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != "keep me" {
		t.Errorf("file changed: %q, %v", b, err)
	}
}

func TestSendWithNothingListening(t *testing.T) {
	if err := Send(sockPath(t), permalink); !errors.Is(err, ErrNotRunning) {
		t.Errorf("Send = %v, want ErrNotRunning", err)
	}
}

func TestPathIsInAPrivateSubdirectory(t *testing.T) {
	if got, want := Path("/data/slk"), filepath.Join("/data/slk", DirName, FileName); got != want {
		t.Errorf("Path = %q, want %q", got, want)
	}
}

// The directory, not the socket's own mode, is what keeps other users
// out: it applies from the moment the socket exists, and on every
// platform. A directory that already exists with a wider mode is
// narrowed.
func TestExistingSocketDirIsMadePrivate(t *testing.T) {
	path := sockPath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	listenT(t, path, (&recorder{}).handle)
	di, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if perm := di.Mode().Perm(); perm != 0o700 {
		t.Errorf("directory mode %o, want 700", perm)
	}
}

// Several instances starting at once over a stale socket: exactly one
// gets it, the rest see ErrInUse, and the winner still answers.
func TestConcurrentStartsOverStaleSocket(t *testing.T) {
	path := sockPath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	_ = ln.Close()

	const n = 8
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		servers []*Server
		inUse   int
	)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := Listen(path, (&recorder{}).handle)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				servers = append(servers, s)
			case errors.Is(err, ErrInUse):
				inUse++
			default:
				t.Errorf("Listen: %v", err)
			}
		}()
	}
	wg.Wait()
	for _, s := range servers {
		t.Cleanup(func() { _ = s.Close() })
	}
	if len(servers) != 1 || inUse != n-1 {
		t.Fatalf("%d listeners, %d ErrInUse; want 1 and %d", len(servers), inUse, n-1)
	}
	if err := Send(path, permalink); err != nil {
		t.Errorf("winner not answering: %v", err)
	}
}

// An instance whose socket file was replaced by another's must not
// delete the replacement when it exits.
func TestCloseLeavesAnotherInstancesSocket(t *testing.T) {
	path := sockPath(t)
	first, err := Listen(path, (&recorder{}).handle)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	second := &recorder{}
	listenT(t, path, second.handle)

	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := Send(path, permalink); err != nil {
		t.Fatalf("second instance's socket gone after the first closed: %v", err)
	}
	if len(second.got()) != 1 {
		t.Errorf("second listener got %q", second.got())
	}
}
