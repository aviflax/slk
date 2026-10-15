package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Two slk processes can save the cache at once, so each save needs its
// own temp file. Whatever sits at the old fixed name (here a directory)
// must not block a save either.
func TestSaveUpdateCacheUsesUniqueTempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "update-check.json")
	if err := os.Mkdir(path+".tmp", 0o700); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- saveUpdateCache(path, updateCache{Latest: "0.26.0"})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("saveUpdateCache: %v", err)
		}
	}
	if got, ok := loadUpdateCache(path); !ok || got.Latest != "0.26.0" {
		t.Errorf("cache = %+v, %v", got, ok)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 2 {
		t.Errorf("directory holds %d entries, want the cache and the pre-existing .tmp only", len(entries))
	}
}

func TestIsNewerVersion(t *testing.T) {
	for _, tc := range []struct {
		latest, current string
		want            bool
	}{
		{"0.26.0", "0.25.0", true},
		{"v0.26.0", "0.25.0", true},
		{"0.26.0", "v0.26.0", false},
		{"0.25.0", "0.26.0", false},
		{"0.10.0", "0.9.9", true},
		{"1.0.0", "0.99.99", true},
		{"0.25.1", "0.25.0", true},
		// A prerelease latest never nags a stable user.
		{"0.26.0-beta.1", "0.25.0", false},
		// A stable release supersedes the prerelease it follows.
		{"0.26.0", "0.26.0-beta.1", true},
		{"0.26.0-beta.2", "0.26.0-beta.1", true},
		{"0.26.0-beta.1", "0.26.0-beta.2", false},
		{"0.26.0-beta.10", "0.26.0-beta.9", true},
		{"0.26.0-rc.1", "0.26.0-beta.9", true},
		// Build metadata is ignored.
		{"0.26.0+abc", "0.26.0", false},
		// Unparseable input never produces a hint.
		{"", "0.25.0", false},
		{"nightly", "0.25.0", false},
		{"0.26.0", "dev", false},
		{"0.26", "0.25.0", false},
	} {
		if got := isNewerVersion(tc.latest, tc.current); got != tc.want {
			t.Errorf("isNewerVersion(%q, %q) = %v, want %v", tc.latest, tc.current, got, tc.want)
		}
	}
}

func TestUpdateCheckableVersion(t *testing.T) {
	for _, v := range []string{"", "dev", "none", "garbage"} {
		if updateCheckableVersion(v) {
			t.Errorf("version %q should skip the update check", v)
		}
	}
	if !updateCheckableVersion("0.25.0") || !updateCheckableVersion("v0.25.0-beta.1") {
		t.Error("release versions should be checked")
	}
}

func TestUpdateCacheFreshness(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name    string
		checked time.Time
		want    bool
	}{
		{"just now", now, true},
		{"23h ago", now.Add(-23 * time.Hour), true},
		{"25h ago", now.Add(-25 * time.Hour), false},
		{"zero", time.Time{}, false},
		{"future (clock skew)", now.Add(48 * time.Hour), false},
	} {
		c := updateCache{CheckedAt: tc.checked, Latest: "0.26.0"}
		if got := c.fresh(now); got != tc.want {
			t.Errorf("%s: fresh = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestUpdateCacheRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "update-check.json")
	if _, ok := loadUpdateCache(path); ok {
		t.Fatal("missing file should not load")
	}
	want := updateCache{CheckedAt: time.Unix(1_700_000_000, 0).UTC(), Latest: "0.26.0"}
	if err := saveUpdateCache(path, want); err != nil {
		t.Fatal(err)
	}
	got, ok := loadUpdateCache(path)
	if !ok || got.Latest != want.Latest || !got.CheckedAt.Equal(want.CheckedAt) {
		t.Errorf("round trip = %+v, %v", got, ok)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := loadUpdateCache(path); ok {
		t.Error("corrupt file should not load")
	}
}

func TestFetchLatestRelease(t *testing.T) {
	var gotUA, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		gotAccept = r.Header.Get("Accept")
		_, _ = w.Write([]byte(`{"tag_name":"v0.26.2","draft":false,"prerelease":false}`))
	}))
	defer srv.Close()
	got, err := fetchLatestRelease(context.Background(), srv.Client(), srv.URL, "0.25.0")
	if err != nil || got != "0.26.2" {
		t.Fatalf("got %q, %v; want 0.26.2", got, err)
	}
	if !strings.HasPrefix(gotUA, "slk/0.25.0") {
		t.Errorf("User-Agent = %q", gotUA)
	}
	if !strings.Contains(gotAccept, "json") {
		t.Errorf("Accept = %q", gotAccept)
	}
}

func TestFetchLatestReleaseErrors(t *testing.T) {
	for name, h := range map[string]http.HandlerFunc{
		"500":      func(w http.ResponseWriter, r *http.Request) { http.Error(w, "x", 500) },
		"403":      func(w http.ResponseWriter, r *http.Request) { http.Error(w, "rate limited", http.StatusForbidden) },
		"bad json": func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("<html>")) },
		"no tag":   func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) },
	} {
		srv := httptest.NewServer(h)
		if _, err := fetchLatestRelease(context.Background(), srv.Client(), srv.URL, "0.25.0"); err == nil {
			t.Errorf("%s: want error", name)
		}
		srv.Close()
	}
}

func TestFetchLatestReleaseTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := fetchLatestRelease(ctx, srv.Client(), srv.URL, "0.25.0"); err == nil {
		t.Error("want timeout error")
	}
}

func TestLatestVersionUsesFreshCacheWithoutNetwork(t *testing.T) {
	now := time.Now()
	path := filepath.Join(t.TempDir(), "u.json")
	if err := saveUpdateCache(path, updateCache{CheckedAt: now.Add(-time.Hour), Latest: "0.30.0"}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	fetch := func(context.Context) (string, error) { calls++; return "9.9.9", nil }
	got, err := latestVersion(context.Background(), path, now, fetch)
	if err != nil || got != "0.30.0" || calls != 0 {
		t.Errorf("got %q, %v, calls=%d", got, err, calls)
	}
}

func TestLatestVersionRefreshesStaleCache(t *testing.T) {
	now := time.Now()
	path := filepath.Join(t.TempDir(), "u.json")
	if err := saveUpdateCache(path, updateCache{CheckedAt: now.Add(-48 * time.Hour), Latest: "0.30.0"}); err != nil {
		t.Fatal(err)
	}
	fetch := func(context.Context) (string, error) { return "0.31.0", nil }
	got, err := latestVersion(context.Background(), path, now, fetch)
	if err != nil || got != "0.31.0" {
		t.Fatalf("got %q, %v", got, err)
	}
	c, _ := loadUpdateCache(path)
	if c.Latest != "0.31.0" || !c.fresh(now) {
		t.Errorf("cache not rewritten: %+v", c)
	}
}

func TestLatestVersionFailureBacksOff(t *testing.T) {
	now := time.Now()
	path := filepath.Join(t.TempDir(), "u.json")
	if err := saveUpdateCache(path, updateCache{CheckedAt: now.Add(-48 * time.Hour), Latest: "0.30.0"}); err != nil {
		t.Fatal(err)
	}
	fetch := func(context.Context) (string, error) { return "", context.DeadlineExceeded }
	got, err := latestVersion(context.Background(), path, now, fetch)
	if err == nil {
		t.Error("want the fetch error")
	}
	// The stale answer survives and the attempt is recorded, so an
	// offline machine does not retry on every launch.
	if got != "0.30.0" {
		t.Errorf("got %q, want stale 0.30.0", got)
	}
	if c, _ := loadUpdateCache(path); !c.fresh(now) || c.Latest != "0.30.0" {
		t.Errorf("failed attempt not recorded: %+v", c)
	}
}

func TestDetectInstallMethod(t *testing.T) {
	env := installEnv{Home: "/home/u"}
	for _, tc := range []struct {
		name string
		exe  string
		env  installEnv
		want installMethod
	}{
		{"apple silicon cask", "/opt/homebrew/Caskroom/slk/0.25.0/slk", env, installBrew},
		{"intel cask", "/usr/local/Caskroom/slk/0.25.0/slk", env, installBrew},
		{"formula cellar", "/opt/homebrew/Cellar/slk/0.25.0/bin/slk", env, installBrew},
		{"linuxbrew", "/home/linuxbrew/.linuxbrew/Caskroom/slk/0.25.0/slk", env, installBrew},
		{"custom brew prefix", "/brew/bin/slk", installEnv{Home: "/home/u", BrewPrefix: "/brew"}, installBrew},
		{"default gopath", "/home/u/go/bin/slk", env, installGo},
		{"custom gopath", "/work/go/bin/slk", installEnv{Home: "/home/u", GOPATH: "/work/go"}, installGo},
		{"multi gopath", "/b/bin/slk", installEnv{Home: "/home/u", GOPATH: "/a:/b"}, installGo},
		{"gobin", "/tools/slk", installEnv{Home: "/home/u", GOBIN: "/tools"}, installGo},
		{"manual", "/usr/local/bin/slk", env, installOther},
		{"home bin", "/home/u/bin/slk", env, installOther},
		{"empty", "", env, installOther},
		{"similar prefix is not a prefix", "/home/u/gopher/bin/slk", env, installOther},
	} {
		if got := detectInstallMethod(tc.exe, tc.env); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestUpdateHint(t *testing.T) {
	for _, tc := range []struct {
		m    installMethod
		want string
	}{
		{installBrew, "brew upgrade --cask slk"},
		{installGo, "https://github.com/kirahowe/slk/releases/latest"},
		{installOther, "https://github.com/kirahowe/slk/releases/latest"},
	} {
		got := updateHint("0.26.2", tc.m)
		if !strings.Contains(got, "0.26.2") || !strings.Contains(got, tc.want) {
			t.Errorf("updateHint(%v) = %q, want version and %q", tc.m, got, tc.want)
		}
	}
}

func TestCachedUpdateHint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "u.json")
	exe := "/opt/homebrew/Caskroom/slk/0.25.0/slk"
	if got := cachedUpdateHint(path, "0.25.0", exe, installEnv{}); got != "" {
		t.Errorf("no cache: got %q", got)
	}
	_ = saveUpdateCache(path, updateCache{CheckedAt: time.Now(), Latest: "0.26.2"})
	if got := cachedUpdateHint(path, "0.25.0", exe, installEnv{}); !strings.Contains(got, "brew upgrade --cask slk") {
		t.Errorf("newer cached: got %q", got)
	}
	if got := cachedUpdateHint(path, "0.26.2", exe, installEnv{}); got != "" {
		t.Errorf("up to date: got %q", got)
	}
	if got := cachedUpdateHint(path, "dev", exe, installEnv{}); got != "" {
		t.Errorf("dev build: got %q", got)
	}
}
