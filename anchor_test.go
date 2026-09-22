package pathguard

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// noIdentity stands for a filesystem whose identities cannot be read: every
// stat fails, so only the name comparison can match.
func noIdentity(t *testing.T) {
	t.Helper()
	saved := statFn
	statFn = func(string) (os.FileInfo, error) { return nil, errors.New("no identity here") }
	t.Cleanup(func() { statFn = saved })
}

func localOf(t *testing.T, home string, protected ...Place) Policy {
	t.Helper()
	p, err := Local(home, protected...)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// A place that does not exist yet is anchored at the directory it would be
// created in. Any spelling of that directory reaches it: the firmlink, and
// /.nofollow (darwin), which the name comparison alone let through and which
// created the real ~/.aws/credentials.
func TestAPlaceThatDoesNotExistYetIsFoundThroughAnySpellingOfItsParent(t *testing.T) {
	home := realTemp(t)
	local := localOf(t, home)
	for _, prefix := range []string{"/System/Volumes/Data", "/.nofollow"} {
		if _, err := os.Stat(prefix + home); err != nil {
			t.Logf("%s: no such spelling here: %v", prefix, err)
			continue
		}
		p := prefix + filepath.Join(home, ".aws", "credentials")
		if reason, _ := local.Check(p); reason != "sensitive_path" {
			t.Errorf("Local.Check(%q) reason = %q, want sensitive_path", p, reason)
		}
	}
}

// ~/.config is a link into a dotfiles repository and ~/.config/gem-agent does
// not exist yet: the repository's spelling of it is the same place.
func TestAPlaceThatDoesNotExistYetIsFoundThroughALinkedParent(t *testing.T) {
	home, dots := realTemp(t), realTemp(t)
	mkdir(t, filepath.Join(dots, "config"))
	link(t, filepath.Join(dots, "config"), filepath.Join(home, ".config"))
	p := filepath.Join(dots, "config", "gem-agent", "config.toml")
	if reason, _ := localOf(t, home).Check(p); reason != "sensitive_path" {
		t.Errorf("Local.Check(%q) reason = %q, want sensitive_path", p, reason)
	}
}

// ~/.ssh/config links into a sync folder whose file does not exist yet:
// creating it there would plant an ssh configuration.
func TestTheMissingTargetOfALinkInsideAFloorDirectoryIsProtected(t *testing.T) {
	home, sync := realTemp(t), realTemp(t)
	mkdir(t, filepath.Join(home, ".ssh"))
	target := filepath.Join(sync, "ssh", "config")
	link(t, target, filepath.Join(home, ".ssh", "config"))
	if reason, _ := localOf(t, home).Check(target); reason != "sensitive_path" {
		t.Errorf("Local.Check(%q) reason = %q, want sensitive_path", target, reason)
	}
}

// A link to / or to the home directory inside a floor directory would make
// everything a protected tree; it protects its own location only.
func TestALinkUpwardsInsideAFloorDirectoryDoesNotProtectEverything(t *testing.T) {
	home := realTemp(t)
	mkdir(t, filepath.Join(home, ".claude"))
	link(t, "/", filepath.Join(home, ".claude", "root"))
	link(t, home, filepath.Join(home, ".claude", "home"))
	link(t, "..", filepath.Join(home, ".claude", "up"))
	write(t, filepath.Join(home, "project", "notes.txt"))
	elsewhere := filepath.Join(realTemp(t), "data.csv")
	local := localOf(t, home)
	for _, p := range []string{filepath.Join(home, "project", "notes.txt"), elsewhere} {
		if _, why := local.Check(p); why != "" {
			t.Errorf("Local.Check(%q) refused: %s", p, why)
		}
	}
	if reason, _ := local.Check(filepath.Join(home, ".claude", "root", "etc", "hosts")); reason != "sensitive_path" {
		t.Error("a path through the link's own location passed")
	}
}

// A server's own directory may link to work directories; its links' targets
// are not made protected.
func TestTheLinksInsideAServerDirectoryAreNotFollowed(t *testing.T) {
	home, srv, work := realTemp(t), realTemp(t), realTemp(t)
	link(t, work, filepath.Join(srv, "latest"))
	local := localOf(t, home, ServerDir(srv, ""))
	if _, why := local.Check(filepath.Join(work, "out.png")); why != "" {
		t.Errorf("a work directory a server directory links to was refused: %s", why)
	}
}

// The target of a link inside a floor directory is protected by identity: a
// hard link to it under another name is refused.
func TestALinkTargetIsProtectedByIdentity(t *testing.T) {
	home, sync, other := realTemp(t), realTemp(t), realTemp(t)
	mkdir(t, filepath.Join(home, ".ssh"))
	target := filepath.Join(sync, "ssh_config")
	write(t, target)
	link(t, target, filepath.Join(home, ".ssh", "config"))
	hard := filepath.Join(other, "innocent.txt")
	if err := os.Link(target, hard); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	if reason, _ := localOf(t, home).Check(hard); reason != "sensitive_path" {
		t.Errorf("a hard link to a link target: reason = %q, want sensitive_path", reason)
	}
}

// Where identity cannot be read, names still protect: a tree place, an exact
// place, a link target, and a place given in a linked spelling.
func TestTheNameComparisonProtectsOnItsOwn(t *testing.T) {
	home, sync, real := realTemp(t), realTemp(t), realTemp(t)
	mkdir(t, filepath.Join(home, ".ssh"))
	target := filepath.Join(sync, "ssh_config")
	write(t, target)
	link(t, target, filepath.Join(home, ".ssh", "config"))
	linked := filepath.Join(realTemp(t), "srv-link")
	link(t, real, linked)
	floor := floorOf(t, home)
	local := localOf(t, home, ServerDir(linked, ""))
	noIdentity(t)
	if reason, _ := local.Check(filepath.Join(home, ".ssh", "id_rsa")); reason != "sensitive_path" {
		t.Errorf("tree place by name: %q", reason)
	}
	if reason, _ := Check(floor, home); reason != "home_dir" {
		t.Errorf("exact place by name: %q", reason)
	}
	if reason, _ := local.Check(target); reason != "sensitive_path" {
		t.Errorf("link target by name: %q", reason)
	}
	if reason, _ := local.Check(filepath.Join(real, "state.json")); reason != "server_dir" {
		t.Errorf("a place given through a link, by its resolved name: %q", reason)
	}
}

// An exact place is found by identity under a spelling no name matches.
func TestAnExactPlaceIsFoundByIdentity(t *testing.T) {
	home := realTemp(t)
	spelled := "/.nofollow" + home
	if _, err := os.Stat(spelled); err != nil {
		t.Skipf("no /.nofollow spelling here: %v", err)
	}
	if reason, _ := Check(floorOf(t, home), spelled); reason != "home_dir" {
		t.Errorf("Check(%q) reason = %q, want home_dir", spelled, reason)
	}
}

// A place that cannot be compared refuses everything rather than protecting
// nothing; a place without words still refuses, in default words.
func TestAPlaceThatCannotBeComparedRefusesEverything(t *testing.T) {
	home := realTemp(t)
	for _, bad := range []Place{ServerDir("", ""), {Path: "relative/dir", Kind: Protected}} {
		if _, err := Local(home, bad); !errors.Is(err, ErrBadPlace) {
			t.Errorf("Local with %q: err = %v, want ErrBadPlace", bad.Path, err)
		}
		if reason, why := Check([]Place{bad}, "/srv/x"); reason != "unconfigured" || why == "" {
			t.Errorf("Check with %q = %q, %q", bad.Path, reason, why)
		}
	}
	dir := realTemp(t)
	if reason, why := Check([]Place{{Path: dir, Kind: Protected}}, filepath.Join(dir, "x")); reason != "protected_path" || why == "" {
		t.Errorf("a place without Reason and Why: %q, %q", reason, why)
	}
}

func TestAPolicyRefusesAPathThatNeverResolves(t *testing.T) {
	dir := realTemp(t)
	link(t, filepath.Join(dir, "b"), filepath.Join(dir, "a"))
	link(t, filepath.Join(dir, "a"), filepath.Join(dir, "b"))
	if reason, _ := localOf(t, realTemp(t)).Check(filepath.Join(dir, "a", "x")); reason != "unresolvable_path" {
		t.Errorf("reason = %q, want unresolvable_path", reason)
	}
}

// Without a working directory a relative path cannot be placed; walking it
// from the root would check the wrong path.
func TestARelativePathWithoutAWorkingDirectoryIsRefused(t *testing.T) {
	saved := getwd
	getwd = func() (string, error) { return "", errors.New("no working directory") }
	t.Cleanup(func() { getwd = saved })
	if reason, _ := localOf(t, realTemp(t)).Check("rel/x"); reason != "unresolvable_path" {
		t.Errorf("reason = %q, want unresolvable_path", reason)
	}
}

// Every full folding in the table equates its rune with its letters, on any
// disk: the disk test above checks only the pairs it can create.
func TestEveryFullFoldingEquatesItsLetters(t *testing.T) {
	for r, letters := range fullFolds {
		if fold(string(r)) != fold(letters) {
			t.Errorf("fold(%q) != fold(%q)", string(r), letters)
		}
	}
	for _, p := range [][2]string{{".conﬁg", ".config"}, {"ẞ", "ss"}, {"ſ", "S"}, {"\u212a", "K"}} {
		if fold(p[0]) != fold(p[1]) {
			t.Errorf("fold(%q) != fold(%q)", p[0], p[1])
		}
	}
}

// Windows opens ".env::$DATA" and ".env." as .env, and a directory through
// "::$INDEX_ALLOCATION"; the name rules see the name Windows opens.
func TestTheNameRulesSeeTheNameWindowsOpens(t *testing.T) {
	saved := windowsNames
	windowsNames = true
	t.Cleanup(func() { windowsNames = saved })
	for _, n := range []string{".env::$DATA", ".env.", ".env ", ".ENV. . "} {
		if !EnvFile(n) {
			t.Errorf("EnvFile(%q) = false", n)
		}
	}
	for _, n := range []string{"id_rsa.", "credentials.json::$DATA", "my-service-account.json "} {
		if !SecretName(n) {
			t.Errorf("SecretName(%q) = false", n)
		}
	}
	if got := CredentialSegment("C:/x/.ssh::$INDEX_ALLOCATION/id_rsa"); got != ".ssh" {
		t.Errorf("CredentialSegment through a stream name = %q, want .ssh", got)
	}
	if !sameName(`C:\Users\u\.ssh.`, `C:\Users\u\.ssh`) {
		t.Error("a trailing dot made a different name")
	}
}

// The cost of one check against a home where every floor directory exists
// and ~/.ssh holds a link, the realistic case.
func BenchmarkLocalCheck(b *testing.B) {
	home, err := filepath.EvalSymlinks(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	for _, d := range credentialDirs {
		if err := os.MkdirAll(filepath.Join(home, filepath.FromSlash(d)), 0o755); err != nil {
			b.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(home, "sync", "config"), filepath.Join(home, ".ssh", "config")); err != nil {
		b.Skip(err)
	}
	local, err := Local(home, ServerDir(filepath.Join(home, ".config", "some-server"), ""))
	if err != nil {
		b.Fatal(err)
	}
	p := filepath.Join(home, "works", "project", "data", "input.csv")
	b.ResetTimer()
	for range b.N {
		local.Check(p, p)
	}
}

// A path longer than any system opens is refused before it costs anything.
func TestAPathLongerThanAnySystemOpensIsRefused(t *testing.T) {
	long := "/" + strings.Repeat("a/", maxPathBytes()/2) + "x"
	if reason, _ := localOf(t, realTemp(t)).Check(long); reason != "unresolvable_path" {
		t.Errorf("reason = %q, want unresolvable_path", reason)
	}
}

// One form costs its segments' stats and nothing quadratic: an agent controls
// the path, and a prepend per segment made a 120 KB path cost 14 s.
func TestLookingAtALongPathAllocatesLinearly(t *testing.T) {
	p := "/" + strings.Repeat("a/", (maxPathBytes()-2)/2) + "x"
	if len(p) > maxPathBytes() {
		t.Fatalf("test path of %d bytes exceeds the cap", len(p))
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	look(p)
	runtime.ReadMemStats(&after)
	segs := len(p) / 2
	// Each ancestor's stat copies its path (about len(p)/2 on average); a
	// prepend per segment adds 16 bytes × segs²/2 on top.
	if got, limit := after.TotalAlloc-before.TotalAlloc, uint64(segs*len(p)); got > limit {
		t.Errorf("look allocated %d bytes for %d segments, over %d", got, segs, limit)
	}
}

// The worst an agent can do with one path argument: the longest path the cap
// lets through, none of it existing.
func BenchmarkLocalCheckLongestPath(b *testing.B) {
	home, err := filepath.EvalSymlinks(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	local, err := Local(home)
	if err != nil {
		b.Fatal(err)
	}
	p := "/" + strings.Repeat("a/", (maxPathBytes()-2)/2) + "x"
	b.ResetTimer()
	for range b.N {
		local.Check(p, p)
	}
}

// ancestors, parent and child are filepath.Dir and filepath.Join without the
// Clean of every prefix; they must give the same strings.
func TestTheLinearWalksAgreeWithFilepath(t *testing.T) {
	for _, p := range []string{"/", "/a", "/a/b/c", "/private/var/folders/x", "/ſ/ﬆ/K", "/a/b/c/d/e/f/g/h"} {
		var want []string
		for cur := p; ; cur = filepath.Dir(cur) {
			want = append(want, cur)
			if filepath.Dir(cur) == cur {
				break
			}
		}
		if got := ancestors(p); !slices.Equal(got, want) {
			t.Errorf("ancestors(%q) = %q, want %q", p, got, want)
		}
		if got, want := parent(p), filepath.Dir(p); got != want {
			t.Errorf("parent(%q) = %q, want %q", p, got, want)
		}
		if got, want := child(p, "x"), filepath.Join(p, "x"); got != want {
			t.Errorf("child(%q, x) = %q, want %q", p, got, want)
		}
	}
}

// A short path through a link whose target is a long relative path grows by
// the target on every hop. Every form is capped, so the chain is refused as
// soon as a form outgrows the cap, not after 40 hops of ever longer forms (a
// 97-byte path cost 21 s).
func TestALinkThatGrowsThePathPastTheCapIsRefusedEarly(t *testing.T) {
	dir := realTemp(t)
	link(t, strings.Repeat("x/", 499)+"x", filepath.Join(dir, "x"))
	p := filepath.Join(dir, "x", "y")
	f, ok := forms(p)
	if ok {
		t.Fatalf("forms(%q) ended; want refused", p)
	}
	for _, form := range f {
		if len(form) > maxPathBytes() {
			t.Errorf("a form of %d bytes was produced, over the cap", len(form))
		}
	}
	stats := 0
	saved := statFn
	statFn = func(s string) (os.FileInfo, error) { stats++; return saved(s) }
	t.Cleanup(func() { statFn = saved })
	if reason, _ := localOf(t, realTemp(t)).Check(p); reason != "unresolvable_path" {
		t.Errorf("reason = %q, want unresolvable_path", reason)
	}
	if stats != 0 {
		t.Errorf("the refused path was still looked at: %d stats", stats)
	}
}

// The worst the cap still allows: the longest chain of links that still ends
// (maxHops-1, planted in a work directory), every form just under the cap, so
// every form is walked and looked at.
func BenchmarkLocalCheckLinkChainAtTheCap(b *testing.B) {
	dir, err := filepath.EvalSymlinks(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	for i := range maxHops - 1 {
		if err := os.Symlink(filepath.Join(dir, fmt.Sprintf("l%02d", i+1)), filepath.Join(dir, fmt.Sprintf("l%02d", i))); err != nil {
			b.Skip(err)
		}
	}
	head := filepath.Join(dir, "l00")
	p := head + "/" + strings.Repeat("a/", (maxPathBytes()-len(head)-3)/2) + "x"
	if f, ok := forms(p); !ok || len(f) < maxHops {
		b.Fatalf("forms: %d, ended %v; want the whole chain, ended", len(f), ok)
	}
	local, err := Local(dir)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for range b.N {
		local.Check(p, p)
	}
}

// A path holding a NUL byte is judged as one string and, once handed to C,
// opened as another (C stops at the NUL): ".netrc\x00.safetensors" opens
// .netrc. It is refused as unresolvable by every entry point.
func TestAPathHoldingANULIsRefused(t *testing.T) {
	home := realTemp(t)
	p := filepath.Join(home, ".netrc") + "\x00.safetensors"
	if reason, _ := localOf(t, home).Check(p); reason != "unresolvable_path" {
		t.Errorf("Local.Check reason = %q, want unresolvable_path", reason)
	}
	if reason, _ := Check(floorOf(t, home), "/srv/x\x00y"); reason != "unresolvable_path" {
		t.Errorf("Check reason = %q, want unresolvable_path", reason)
	}
}
