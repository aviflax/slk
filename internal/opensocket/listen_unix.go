//go:build darwin || linux || freebsd || netbsd || openbsd || dragonfly

package opensocket

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// listen binds the socket at path and returns the listener with the
// function that stops it.
//
// Access control is the directory: the socket lives in a subdirectory
// of its own (DirName) that listen keeps at 0700 and owned by this
// user, so no other user can reach the socket from the moment it
// exists. That beats narrowing the process umask around net.Listen,
// which is process-wide and would race every other goroutine creating
// files, and beats chmod after bind, which leaves a window. It also
// does not depend on the OS honouring a socket file's own mode, which
// BSD-derived systems have not always done. The socket is still
// chmodded to 0600 afterwards.
//
// Clearing a stale socket and binding happen under an exclusive flock
// on path+".lock", so two instances starting together over a stale
// socket cannot both decide it is stale and both bind. The lock file
// is left in place: removing it would reopen exactly that race.
func listen(path string) (net.Listener, func() error, error) {
	if err := privateDir(filepath.Dir(path)); err != nil {
		return nil, nil, err
	}
	lockPath := path + ".lock"
	unlock, err := lockFile(lockPath)
	if err != nil {
		return nil, nil, err
	}
	defer unlock()

	if err := clearStale(path); err != nil {
		return nil, nil, err
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, nil, fmt.Errorf("listening on %s: %w", path, err)
	}
	// Close removes the file itself, and only if it is still ours.
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	ours, err := os.Lstat(path)
	if err == nil {
		err = os.Chmod(path, 0o600)
	}
	if err != nil {
		_ = ln.Close()
		_ = os.Remove(path)
		return nil, nil, fmt.Errorf("securing socket: %w", err)
	}

	stop := func() error {
		// Under the same lock, so a starting instance cannot clear and
		// rebind between the check and the remove.
		if unlock, err := lockFile(lockPath); err == nil {
			defer unlock()
		}
		if cur, err := os.Lstat(path); err == nil && os.SameFile(cur, ours) {
			_ = os.Remove(path)
		}
		return ln.Close()
	}
	return ln, stop, nil
}

// privateDir creates dir (0700) or checks the existing one: it must be
// a real directory owned by this user, and is narrowed to 0700.
func privateDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating socket directory: %w", err)
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("checking socket directory: %w", err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && int(st.Uid) != os.Getuid() {
		return fmt.Errorf("%s is owned by another user", dir)
	}
	if fi.Mode().Perm() != 0o700 {
		if err := os.Chmod(dir, 0o700); err != nil {
			return fmt.Errorf("restricting socket directory: %w", err)
		}
	}
	return nil
}

// lockFile takes an exclusive flock on path, creating it if needed,
// and returns the function that releases it.
func lockFile(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening socket lock: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("locking socket: %w", err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}

// clearStale removes a socket at path that nobody answers on. It
// returns ErrInUse when something answers and refuses to remove a file
// that is not a socket.
func clearStale(path string) error {
	fi, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("checking %s: %w", path, err)
	}
	if fi.Mode()&fs.ModeSocket == 0 {
		return fmt.Errorf("%s exists and is not a socket; not replacing it", path)
	}
	if conn, err := net.DialTimeout("unix", path, 500*time.Millisecond); err == nil {
		_ = conn.Close()
		return ErrInUse
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("removing stale socket: %w", err)
	}
	return nil
}
