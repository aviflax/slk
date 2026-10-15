package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Update check: in the background, ask GitHub for the latest slk release
// and tell the user in the UI when it is newer than the running binary.
// The answer is cached on disk for updateCheckInterval, so --version can
// report it without a network call and several running slk processes
// share one request.

const (
	latestReleaseURL    = "https://api.github.com/repos/kirahowe/slk/releases/latest"
	updateCheckInterval = 6 * time.Hour
	// updateRecheckInterval is how often a running slk consults the
	// cache again. slk stays open for days, so a check at startup alone
	// would miss every release after it; the cache still limits network
	// requests to one per updateCheckInterval.
	updateRecheckInterval = time.Hour
	updateCheckTimeout    = 5 * time.Second
	// updateToastDelay holds the toast back until the loading overlay is
	// likely gone, so the hint is not shown behind it.
	updateToastDelay = 4 * time.Second
)

// updateCheckableVersion reports whether v is a real release build.
// Builds without an injected version ("dev") never check.
func updateCheckableVersion(v string) bool {
	switch v {
	case "", "dev", "none":
		return false
	}
	_, ok := parseSemver(v)
	return ok
}

type semver struct {
	major, minor, patch int
	pre                 []string
}

func parseSemver(s string) (semver, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	var v semver
	if i := strings.IndexByte(s, '-'); i >= 0 {
		v.pre = strings.Split(s[i+1:], ".")
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return v, false
	}
	nums := [3]int{}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v, false
		}
		nums[i] = n
	}
	v.major, v.minor, v.patch = nums[0], nums[1], nums[2]
	return v, true
}

// comparePre orders prerelease identifier lists per semver: a release
// (no identifiers) outranks any prerelease; numeric identifiers compare
// numerically and sort below alphanumeric ones.
func comparePre(a, b []string) int {
	switch {
	case len(a) == 0 && len(b) == 0:
		return 0
	case len(a) == 0:
		return 1
	case len(b) == 0:
		return -1
	}
	for i := 0; i < len(a) && i < len(b); i++ {
		an, aerr := strconv.Atoi(a[i])
		bn, berr := strconv.Atoi(b[i])
		switch {
		case aerr == nil && berr == nil:
			if an != bn {
				return cmpInt(an, bn)
			}
		case aerr == nil:
			return -1
		case berr == nil:
			return 1
		case a[i] != b[i]:
			return strings.Compare(a[i], b[i])
		}
	}
	return cmpInt(len(a), len(b))
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// isNewerVersion reports whether latest is a newer release than current.
// A prerelease latest is ignored unless current is itself a prerelease.
// Anything unparseable is never newer.
func isNewerVersion(latest, current string) bool {
	l, ok := parseSemver(latest)
	if !ok {
		return false
	}
	c, ok := parseSemver(current)
	if !ok {
		return false
	}
	if len(l.pre) > 0 && len(c.pre) == 0 {
		return false
	}
	if d := cmpInt(l.major, c.major); d != 0 {
		return d > 0
	}
	if d := cmpInt(l.minor, c.minor); d != 0 {
		return d > 0
	}
	if d := cmpInt(l.patch, c.patch); d != 0 {
		return d > 0
	}
	return comparePre(l.pre, c.pre) > 0
}

// updateCache is the on-disk record of the last check. CheckedAt is the
// last attempt, successful or not.
type updateCache struct {
	CheckedAt time.Time `json:"checked_at"`
	Latest    string    `json:"latest"`
}

func (c updateCache) fresh(now time.Time) bool {
	age := now.Sub(c.CheckedAt)
	return !c.CheckedAt.IsZero() && age >= 0 && age < updateCheckInterval
}

func updateCachePath() string { return filepath.Join(xdgCache(), "update-check.json") }

func loadUpdateCache(path string) (updateCache, bool) {
	var c updateCache
	b, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(b, &c) != nil {
		return updateCache{}, false
	}
	return c, true
}

func saveUpdateCache(path string, c updateCache) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	// A temp file of its own (CreateTemp, mode 0600) in the target's
	// directory, so two slk processes saving at once cannot write the
	// same file, and the rename stays on one filesystem.
	f, err := os.CreateTemp(filepath.Dir(path), ".update-check-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp) // no-op once the rename succeeds
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// fetchLatestRelease returns the latest stable release's version (no
// "v" prefix) from the GitHub releases API at url.
func fetchLatestRelease(ctx context.Context, client *http.Client, url, current string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "slk/"+current+" (+https://github.com/kirahowe/slk)")
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github releases: %s", resp.Status)
	}
	var body struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return "", err
	}
	v := strings.TrimPrefix(body.TagName, "v")
	if _, ok := parseSemver(v); !ok {
		return "", fmt.Errorf("github releases: unusable tag %q", body.TagName)
	}
	return v, nil
}

// latestVersion returns the latest known release. A cache younger than
// updateCheckInterval answers without the network. Otherwise fetch runs, and the attempt
// is recorded either way so an offline machine does not retry on every
// launch; on failure the previous answer (possibly empty) is returned
// alongside the error.
func latestVersion(ctx context.Context, cachePath string, now time.Time, fetch func(context.Context) (string, error)) (string, error) {
	cached, _ := loadUpdateCache(cachePath)
	if cached.fresh(now) {
		return cached.Latest, nil
	}
	latest, err := fetch(ctx)
	if err != nil {
		cached.CheckedAt = now
		_ = saveUpdateCache(cachePath, cached)
		return cached.Latest, err
	}
	_ = saveUpdateCache(cachePath, updateCache{CheckedAt: now, Latest: latest})
	return latest, nil
}

type installMethod int

const (
	installOther installMethod = iota
	installBrew
	installGo
)

// installEnv carries the environment detectInstallMethod consults, so
// tests inject it.
type installEnv struct {
	Home, GOPATH, GOBIN, BrewPrefix string
}

func currentInstallEnv() installEnv {
	home, _ := os.UserHomeDir()
	return installEnv{
		Home:       home,
		GOPATH:     os.Getenv("GOPATH"),
		GOBIN:      os.Getenv("GOBIN"),
		BrewPrefix: os.Getenv("HOMEBREW_PREFIX"),
	}
}

func underDir(path, dir string) bool {
	if dir == "" {
		return false
	}
	dir = filepath.Clean(dir)
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}

// detectInstallMethod classifies a symlink-resolved executable path.
func detectInstallMethod(exe string, env installEnv) installMethod {
	if exe == "" {
		return installOther
	}
	exe = filepath.Clean(exe)
	slashed := filepath.ToSlash(exe)
	if strings.Contains(slashed, "/Caskroom/") || strings.Contains(slashed, "/Cellar/") ||
		underDir(exe, env.BrewPrefix) {
		return installBrew
	}
	if underDir(exe, env.GOBIN) {
		return installGo
	}
	gopath := env.GOPATH
	if gopath == "" && env.Home != "" {
		gopath = filepath.Join(env.Home, "go")
	}
	for _, p := range filepath.SplitList(gopath) {
		if underDir(exe, filepath.Join(p, "bin")) {
			return installGo
		}
	}
	return installOther
}

// resolvedExecutable is the running binary's path with symlinks
// followed, so a brew shim in /opt/homebrew/bin resolves into Caskroom.
func resolvedExecutable() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe
}

// updateHint is the user-facing text for an available release.
func updateHint(latest string, m installMethod) string {
	prefix := fmt.Sprintf("slk %s is available. ", latest)
	switch m {
	case installBrew:
		return prefix + "Run: brew upgrade --cask slk"
	}
	// No go install hint: the module path is still github.com/gammons/slk,
	// so @latest would install upstream rather than this fork's release.
	return prefix + "Get it at https://github.com/kirahowe/slk/releases/latest"
}

// cachedUpdateHint builds the hint from the cache alone (no network),
// or returns "" when there is nothing newer to report.
func cachedUpdateHint(cachePath, current, exe string, env installEnv) string {
	if !updateCheckableVersion(current) {
		return ""
	}
	c, ok := loadUpdateCache(cachePath)
	if !ok || !isNewerVersion(c.Latest, current) {
		return ""
	}
	return updateHint(strings.TrimPrefix(c.Latest, "v"), detectInstallMethod(exe, env))
}

// versionUpdateLine is the extra line `slk --version` prints, or "".
func versionUpdateLine() string {
	return cachedUpdateHint(updateCachePath(), version, resolvedExecutable(), currentInstallEnv())
}

// checkForUpdate runs the cached check and returns the hint to show, or
// "" when up to date or the check failed, plus the new version. Safe to
// call from a goroutine.
func checkForUpdate(ctx context.Context, current string) (latestVer, hint string) {
	ctx, cancel := context.WithTimeout(ctx, updateCheckTimeout)
	defer cancel()
	client := &http.Client{Timeout: updateCheckTimeout}
	latest, _ := latestVersion(ctx, updateCachePath(), time.Now(), func(ctx context.Context) (string, error) {
		return fetchLatestRelease(ctx, client, latestReleaseURL, current)
	})
	if !isNewerVersion(latest, current) {
		return "", ""
	}
	latest = strings.TrimPrefix(latest, "v")
	return latest, updateHint(latest, detectInstallMethod(resolvedExecutable(), currentInstallEnv()))
}

// watchForUpdates runs check now and then every interval until ctx ends,
// calling notify once for each newer version it finds. The first notify
// waits firstDelay, so a hint found at startup is not drawn behind the
// loading overlay.
func watchForUpdates(ctx context.Context, interval, firstDelay time.Duration, check func(context.Context) (latest, hint string), notify func(latest, hint string)) {
	var told string
	delay := firstDelay
	for {
		if latest, hint := check(ctx); hint != "" && latest != told {
			if !sleepCtx(ctx, delay) {
				return
			}
			told = latest
			notify(latest, hint)
		}
		delay = 0
		if !sleepCtx(ctx, interval) {
			return
		}
	}
}

// sleepCtx waits d, or returns false as soon as ctx ends.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
