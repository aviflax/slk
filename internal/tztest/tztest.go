// Package tztest runs part of a test under another local time zone, in
// a child process of the test binary started with TZ set.
//
// A test must not set time.Local itself: every time.Now, in every
// goroutine, reads it, so a write races with the timers earlier tests
// left running (a toast's clear tick, say) and go test -race reports
// it. A child process gets its zone from TZ at start-up and shares
// nothing with the parent.
//
// Only _test.go files may import it. It is an ordinary package so that
// tests in every package can reach it.
package tztest

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// The environment of a child: the zone it runs body for, and the file
// it writes body's result to.
const (
	zoneEnv = "SLK_TZTEST_ZONE"
	outEnv  = "SLK_TZTEST_OUT"
)

// Each runs body once per zone (an IANA name such as "Asia/Tokyo"),
// each time in a child process of the test binary with TZ=zone, and
// returns what body returned there, in zones' order. Each child runs
// only the calling test; a failure in body fails the calling test with
// the child's output. A zone the host's zoneinfo lacks is skipped in
// the child, and its result is "".
//
// In the child, Each runs body for the child's zone and skips the rest
// of the test, so the code after Each runs in the parent alone.
func Each(t *testing.T, zones []string, body func(t *testing.T) string) []string {
	t.Helper()
	if zone, child := os.LookupEnv(zoneEnv); child {
		for _, z := range zones {
			if z == zone {
				runChild(t, zone, body)
			}
		}
		t.SkipNow()
	}
	out := make([]string, len(zones))
	errs := make([]error, len(zones))
	dir := t.TempDir()
	// The children run at once: each starts the test binary, which
	// under -race takes about a second, and they share nothing.
	var wg sync.WaitGroup
	for i, zone := range zones {
		wg.Add(1)
		go func() {
			defer wg.Done()
			path := filepath.Join(dir, fmt.Sprintf("zone%d", i))
			cmd := exec.Command(os.Args[0], "-test.run="+runPattern(t.Name()), "-test.count=1", "-test.v")
			cmd.Env = append(os.Environ(), "TZ="+zone, zoneEnv+"="+zone, outEnv+"="+path)
			if b, err := cmd.CombinedOutput(); err != nil {
				errs[i] = fmt.Errorf("under TZ=%s: %w\n%s", zone, err, b)
				return
			}
			b, err := os.ReadFile(path)
			if err != nil && !os.IsNotExist(err) {
				errs[i] = fmt.Errorf("under TZ=%s: reading the result: %w", zone, err)
				return
			}
			out[i] = string(b)
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// runChild runs body in the child for zone and writes its result.
func runChild(t *testing.T, zone string, body func(t *testing.T) string) {
	t.Helper()
	// The host's zoneinfo, read at start-up, must have the zone, or
	// time.Local is silently UTC and the test proves nothing.
	want, err := time.LoadLocation(zone)
	if err != nil || time.Local.String() != want.String() {
		t.Skipf("host zoneinfo has no %s (time.Local = %s)", zone, time.Local)
	}
	got := body(t)
	if err := os.WriteFile(os.Getenv(outEnv), []byte(got), 0o600); err != nil {
		t.Fatalf("writing the result: %v", err)
	}
}

// runPattern is a -test.run pattern for the test named name, a subtest
// included ("TestX/case").
func runPattern(name string) string {
	parts := strings.Split(name, "/")
	for i, p := range parts {
		parts[i] = "^" + regexp.QuoteMeta(p) + "$"
	}
	return strings.Join(parts, "/")
}
