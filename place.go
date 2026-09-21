package pathguard

import (
	"os"
	"path/filepath"
	"strings"
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

// Check reports the first place any form of any of paths lies in — its reason
// and sentence — or two empty strings. A path whose chain of links does not
// end is refused with reason "unresolvable_path".
//
// Every place is compared with every form twice, and a match either way
// counts: by identity (os.SameFile against the form and each existing
// directory above it), which catches every spelling of a place that exists —
// case on a case-insensitive disk, links, normalisation — and by name,
// case-folded, which still covers a place that does not exist yet and a
// filesystem whose inode numbers cannot be trusted. An Exact place matches the
// form itself only, never an ancestor: otherwise "/" would refuse everything.
func Check(places []Place, paths ...string) (reason, why string) {
	views, ok := viewsOf(paths)
	if !ok {
		return "unresolvable_path", "a chain of links on the path does not end, or a link on it cannot be read"
	}
	for _, pl := range prepare(places) {
		for _, v := range views {
			if pl.holds(v) {
				return pl.Reason, pl.Why
			}
		}
	}
	return "", ""
}

// view is one form ready for comparison: its cleaned spelling, its own
// identity, and the identities of it and every existing directory above it.
type view struct {
	path  string
	self  os.FileInfo
	chain []os.FileInfo
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
	for cur := v.path; ; {
		if fi, err := os.Stat(cur); err == nil {
			if cur == v.path {
				v.self = fi
			}
			v.chain = append(v.chain, fi)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return v
		}
		cur = parent
	}
}

// prepared is a Place looked up once for one check: its spellings for the name
// comparison (as given and resolved) and its identity, nil when it does not
// exist.
type prepared struct {
	Place
	spellings []string
	info      os.FileInfo
}

func prepare(places []Place) []prepared {
	out := make([]prepared, 0, len(places))
	for _, pl := range places {
		if pl.Path == "" {
			continue
		}
		p := prepared{Place: pl, spellings: []string{filepath.Clean(pl.Path)}}
		if r, err := filepath.EvalSymlinks(pl.Path); err == nil && filepath.Clean(r) != p.spellings[0] {
			p.spellings = append(p.spellings, filepath.Clean(r))
		}
		if fi, err := os.Stat(pl.Path); err == nil {
			p.info = fi
		}
		out = append(out, p)
	}
	return out
}

func (pl prepared) holds(v view) bool {
	if pl.Exact {
		if pl.info != nil && v.self != nil && os.SameFile(pl.info, v.self) {
			return true
		}
		for _, s := range pl.spellings {
			if strings.EqualFold(v.path, s) {
				return true
			}
		}
		return false
	}
	if pl.info != nil {
		for _, fi := range v.chain {
			if os.SameFile(pl.info, fi) {
				return true
			}
		}
	}
	for _, s := range pl.spellings {
		if withinFold(v.path, s) {
			return true
		}
	}
	return false
}

// withinFold reports whether path is root or lies under it, ignoring case: a
// sibling that merely shares the prefix (/data-evil against /data) is not
// inside.
func withinFold(path, root string) bool {
	if strings.EqualFold(path, root) {
		return true
	}
	r := strings.TrimSuffix(root, string(filepath.Separator)) + string(filepath.Separator)
	return len(path) > len(r) && strings.EqualFold(path[:len(r)], r)
}
