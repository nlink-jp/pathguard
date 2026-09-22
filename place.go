package pathguard

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Kind says what a Place is, so a caller can choose places by what they are
// rather than by the wording of their reasons.
type Kind int

const (
	// System is a system location, or the home directory itself: it may not be
	// a work directory, but reading a file there is not a leak.
	System Kind = iota + 1
	// Credential is a place that holds secrets.
	Credential
	// AgentControl is a place that steers an agent: its configuration.
	AgentControl
	// Protected is a directory a particular server guards: its own
	// configuration and state, or a browser profile it drives.
	Protected
)

// Place is a location a path may not lie in: the directory (or file) and
// everything under it, or — Exact — only the path itself.
type Place struct {
	Path   string
	Exact  bool
	Kind   Kind
	Reason string // for a structured refusal's details.reason, e.g. "sensitive_path"
	Why    string // the sentence a refusal gives
}

// ServerDir is the Place for a server's own configuration or state directory,
// in the fleet's wording. note, when given, says what it holds — "which holds
// its configuration and its OAuth tokens".
func ServerDir(path, note string) Place {
	why := "it is inside this server's own directory " + path
	if note != "" {
		why += ", " + note
	}
	return Place{Path: path, Kind: Protected, Reason: "server_dir", Why: why}
}

// ErrBadPlace is returned, and every check refused, when a place has no
// absolute path: a place that cannot be compared must not silently protect
// nothing.
var ErrBadPlace = errors.New("a protected place has no absolute path, so nothing can be checked against it")

// validPlaces reports the first place that cannot be compared.
func validPlaces(places []Place) error {
	for _, p := range places {
		if p.Path == "" || !filepath.IsAbs(p.Path) {
			return fmt.Errorf("%w (%q)", ErrBadPlace, p.Path)
		}
	}
	return nil
}

// Check reports the first place any form of any of paths lies in — its reason
// and sentence — or two empty strings. A path whose chain of links does not
// end is refused with reason "unresolvable_path"; a place without an absolute
// path refuses every path, with reason "unconfigured".
//
// Every place is compared with every form twice, and a match either way
// counts. By identity: the place is anchored at the deepest part of its path
// that exists — the place itself, or the directory it would be created in —
// together with the names of the rest, and a form matches when one of its own
// existing ancestors is the same file and its remaining names begin with the
// place's. That catches every spelling of the existing part — case on a
// case-insensitive disk, links, firmlinks, /.nofollow, /.vol, normalisation —
// whether or not the place itself exists yet. And by name, folded, which still
// protects on a filesystem whose inode numbers cannot be trusted. An Exact
// place matches the form itself only, never an ancestor: otherwise "/" would
// refuse everything.
func Check(places []Place, paths ...string) (reason, why string) {
	if err := validPlaces(places); err != nil {
		return "unconfigured", err.Error()
	}
	views, ok := viewsOf(paths)
	if !ok {
		return unresolvable()
	}
	return checkViews(places, views)
}

func unresolvable() (reason, why string) {
	return "unresolvable_path", "a chain of links on the path does not end, or a link on it cannot be read"
}

func checkViews(places []Place, views []view) (reason, why string) {
	for _, pl := range prepare(places) {
		for _, v := range views {
			if pl.holds(v) {
				return pl.Reason, pl.Why
			}
		}
	}
	return "", ""
}

// statFn is os.Stat; a test replaces it to stand for a filesystem whose
// identities cannot be read, and so proves the name comparison on its own.
var statFn = os.Stat

// anchor is an existing ancestor-or-self of a path, and the path's remaining
// segments below it, each folded (segKey). rest is empty when the anchor is
// the path itself.
type anchor struct {
	info os.FileInfo
	rest []string
}

// view is one form ready for comparison: its cleaned spelling, and an anchor
// for every existing ancestor-or-self, deepest first.
type view struct {
	path    string
	anchors []anchor
}

func viewsOf(paths []string) ([]view, bool) {
	var views []view
	ok := true
	for _, p := range paths {
		f, fok := forms(p)
		ok = ok && fok
		for _, form := range f {
			views = append(views, look(form))
		}
	}
	return views, ok
}

func look(form string) view {
	v := view{path: filepath.Clean(form)}
	var rest []string
	for cur := v.path; ; {
		if fi, err := statFn(cur); err == nil {
			v.anchors = append(v.anchors, anchor{info: fi, rest: append([]string(nil), rest...)})
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return v
		}
		rest = append([]string{segKey(filepath.Base(cur))}, rest...)
		cur = parent
	}
}

// prepared is a Place looked up once for one check: every form of its path,
// for the name comparison, and each form's deepest anchor, for identity.
type prepared struct {
	Place
	spellings []string
	anchors   []anchor
}

func prepare(places []Place) []prepared {
	out := make([]prepared, 0, len(places))
	for _, pl := range places {
		p := prepareOne(pl)
		out = append(out, p)
		out = append(out, linkTargets(p)...)
	}
	return out
}

func prepareOne(pl Place) prepared {
	if pl.Reason == "" {
		pl.Reason = "protected_path"
	}
	if pl.Why == "" {
		pl.Why = "it is inside the protected location " + pl.Path
	}
	p := prepared{Place: pl}
	fs, _ := forms(pl.Path)
	for _, f := range fs {
		p.spellings = append(p.spellings, f)
		if v := look(f); len(v.anchors) > 0 {
			p.anchors = append(p.anchors, v.anchors[0])
		}
	}
	return p
}

// linkTargets protects where the links directly inside a credential or
// agent-control directory lead. A link's own location is inside the place and
// already protected; its target need not be. On the machine that found it,
// ~/.ssh/config links into a sync folder, and the sync folder's copy — the same
// bytes — was readable by naming it; a dangling one could be created there.
// Only a place's own entries are read, one directory per place per check;
// links deeper inside are not followed (a documented limit). A target that is
// the place itself or lies above it (a link to / or to the home directory) is
// skipped: it would make the place's parents a protected tree and refuse
// everything. Other places are skipped: a system tree's links lead to more
// system files, and a server's own directory may link to work directories.
func linkTargets(p prepared) []prepared {
	if p.Exact || p.Kind != Credential && p.Kind != AgentControl {
		return nil
	}
	entries, err := os.ReadDir(p.Path)
	if err != nil {
		return nil
	}
	var above []os.FileInfo
	for _, a := range look(p.Path).anchors {
		above = append(above, a.info)
	}
	var out []prepared
	for _, e := range entries {
		if !linkMode(e.Type()) {
			continue
		}
		at := filepath.Join(p.Path, e.Name())
		target, err := os.Readlink(at)
		if err != nil {
			continue
		}
		t := prepareOne(Place{
			Path:   joinTarget(p.Path, target),
			Kind:   p.Kind,
			Reason: p.Reason,
			Why:    "it is where " + at + " leads, and " + p.Why,
		})
		if !reachesUp(t, above) {
			out = append(out, t)
		}
	}
	return out
}

// reachesUp reports whether an existing form of t is one of above.
func reachesUp(t prepared, above []os.FileInfo) bool {
	for _, a := range t.anchors {
		if len(a.rest) != 0 {
			continue
		}
		for _, fi := range above {
			if os.SameFile(a.info, fi) {
				return true
			}
		}
	}
	return false
}

// linkMode reports whether an entry may be a link to follow: a symbolic link,
// or on Windows any reparse point Go reports as irregular (a junction since
// Go 1.23), which os.Readlink then reads or rejects.
func linkMode(m os.FileMode) bool {
	return m&os.ModeSymlink != 0 || runtime.GOOS == "windows" && m&os.ModeIrregular != 0
}

func (pl prepared) holds(v view) bool {
	for _, a := range pl.anchors {
		for _, b := range v.anchors {
			if !os.SameFile(a.info, b.info) {
				continue
			}
			if pl.Exact && equalSegs(b.rest, a.rest) || !pl.Exact && prefixSegs(b.rest, a.rest) {
				return true
			}
		}
	}
	for _, s := range pl.spellings {
		if pl.Exact && sameName(v.path, s) || !pl.Exact && withinFold(v.path, s) {
			return true
		}
	}
	return false
}

func equalSegs(a, b []string) bool {
	return len(a) == len(b) && prefixSegs(a, b)
}

// prefixSegs reports whether segs begins with prefix, segment by segment.
func prefixSegs(segs, prefix []string) bool {
	if len(prefix) > len(segs) {
		return false
	}
	for i := range prefix {
		if segs[i] != prefix[i] {
			return false
		}
	}
	return true
}
