package pathguard

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// maxHops bounds how many links one path may pass through before it is
// refused as unresolvable. The kernels' own limits are of the same order
// (32 on darwin, 40 on Linux).
const maxHops = 40

// Forms returns every spelling of p worth checking: p as given (made absolute
// and cleaned), every path on the way — p with its first link replaced by that
// link's target, then the next, one hop at a time — and the final path.
//
// The paths on the way are what a comparison of end points misses. On a
// machine where ~/.ssh is a real directory but ~/.ssh/config links into a sync
// folder, a link planted as work/x → ~/.ssh/config resolves somewhere that is
// neither ~/.ssh nor under it; only the middle form, ~/.ssh/config, is.
//
// A link's location on its own is not a form when it is only a prefix of the
// path: /var → /private/var is met on the way to every darwin temporary
// directory, and "/var" is not the path being asked about.
func Forms(p string) []string {
	f, _ := forms(p)
	return f
}

// maxPathBytes bounds every form, the given path and each one a link hop
// produces: a short path through a link with a long relative target grows by
// the target's length on every hop, and every form is walked and looked at.
// A longer path cannot be opened by its path at all — PATH_MAX is 4096 bytes
// on Linux and 1024 on darwin, and Windows allows 32,767 UTF-16 units with the
// \\?\ prefix — so refusing it refuses nothing a server could have opened.
func maxPathBytes() int {
	if runtime.GOOS == "windows" {
		return 32 << 10
	}
	return 4096
}

// forms is Forms with a verdict on whether the chain of links ended. A chain
// longer than maxHops, a link that cannot be read, a form longer than
// maxPathBytes, or a path holding a NUL byte is ok=false, and the caller
// refuses rather than guess where the path leads.
//
// A NUL is in no path any system opens, but a path handed to C (C.CString)
// ends at the first one: ".netrc\x00.safetensors" is judged as one string and
// opened as another.
func forms(p string) (out []string, ok bool) {
	if p == "" {
		return nil, true
	}
	if strings.IndexByte(p, 0) >= 0 {
		return nil, false
	}
	seen := map[string]bool{}
	add := func(s string) {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	cur, ok := absolute(p)
	if !ok || len(cur) > maxPathBytes() {
		return nil, false
	}
	add(filepath.Clean(cur))
	for range maxHops {
		r := step(cur)
		if !r.ok {
			return out, false
		}
		if r.link == "" && !r.again {
			add(r.final)
			return out, true
		}
		if len(r.next) > maxPathBytes() {
			return out, false
		}
		add(filepath.Clean(r.next))
		cur = r.next
	}
	return out, false
}

// getwd is os.Getwd; a test replaces it with one that fails.
var getwd = os.Getwd

// absolute joins a relative path onto the working directory without cleaning
// it, so a ".." in it is resolved against real directories by step, not
// cancelled by name. A working directory that cannot be read is ok=false: the
// path would otherwise be walked from the root.
//
// Windows normalises every path by name before opening it — ".." applied
// lexically, the trailing dots and spaces of the last name dropped, rooted and
// drive-relative forms (\Users, C:foo) completed — so there every path, not
// only a relative one, goes through filepath.Abs (GetFullPathName), which is
// that normalisation.
func absolute(p string) (string, bool) {
	if runtime.GOOS == "windows" {
		a, err := filepath.Abs(p)
		return a, err == nil
	}
	if filepath.IsAbs(p) {
		return p, true
	}
	wd, err := getwd()
	if err != nil {
		return "", false
	}
	return wd + string(filepath.Separator) + p, true
}

// joinTarget is where a link in dir whose target is target leads, not yet
// cleaned. On Windows a target rooted without a drive (\Users\u) is on the
// link's own volume, not under dir.
func joinTarget(dir, target string) string {
	if filepath.IsAbs(target) {
		return target
	}
	if runtime.GOOS == "windows" && filepath.VolumeName(target) == "" && target != "" && os.IsPathSeparator(target[0]) {
		return filepath.VolumeName(dir) + target
	}
	return dir + string(filepath.Separator) + target
}

type stepResult struct {
	link  string // the first link met: its own location
	next  string // p with that link replaced by its target, not yet cleaned
	final string // when there is no link: the resolved path
	again bool   // next is to be walked again (a ".." after a missing component)
	ok    bool
}

// step walks p from the root one component at a time and never cleans a
// component it has not resolved: "." and ".." apply to the part already
// walked, which holds no link, so in p2/../q the link p2 is followed before
// anything climbs out of it. It stops at the first link, or at the first
// component that does not exist — nothing below that can be a link, so the
// rest is joined by name. walked is extended and shortened as a string
// (child, parent), never re-cleaned, so a walk is linear in p's length.
func step(p string) stepResult {
	sep := string(filepath.Separator)
	vol := filepath.VolumeName(p)
	walked := vol + sep
	parts := split(p[len(vol):])
	for i, comp := range parts {
		switch comp {
		case ".":
			continue
		case "..":
			walked = parent(walked)
			continue
		}
		cand := child(walked, comp)
		fi, err := os.Lstat(cand)
		if err != nil {
			rest := filepath.Clean(filepath.Join(append([]string{cand}, parts[i+1:]...)...))
			// A ".." after the missing component climbs back into what exists,
			// and a link there would be missed if the rest were joined by name.
			// Resolve the cleaned remainder again: it is what Windows opens (it
			// applies ".." by name), and on Unix the open fails anyway.
			for _, later := range parts[i+1:] {
				if later == ".." {
					return stepResult{next: rest, again: true, ok: true}
				}
			}
			return stepResult{final: rest, ok: true}
		}
		if !linkMode(fi.Mode()) {
			walked = cand
			continue
		}
		target, err := os.Readlink(cand)
		if err != nil {
			if fi.Mode()&os.ModeSymlink == 0 {
				// A Windows reparse point that is not a link (a cloud-file
				// placeholder, a deduplicated file) is an ordinary entry.
				walked = cand
				continue
			}
			return stepResult{}
		}
		next := joinTarget(walked, target)
		if rest := strings.Join(parts[i+1:], sep); rest != "" {
			next += sep + rest
		}
		if runtime.GOOS == "windows" {
			// Windows applies ".." in the combined path by name, as it did in
			// the path it was given.
			next = filepath.Clean(next)
		}
		return stepResult{link: cand, next: next, ok: true}
	}
	return stepResult{final: filepath.Clean(walked), ok: true}
}

// child is dir/comp for a walked directory and one component, which holds no
// separator and is neither "." nor "..": filepath.Join without its Clean.
func child(dir, comp string) string {
	if os.IsPathSeparator(dir[len(dir)-1]) {
		return dir + comp
	}
	return dir + string(filepath.Separator) + comp
}

// parent is filepath.Dir of a walked directory, which is clean: its prefix up
// to the last separator, or the root.
func parent(dir string) string {
	vol := len(filepath.VolumeName(dir))
	i := len(dir) - 1
	for i > vol && !os.IsPathSeparator(dir[i]) {
		i--
	}
	if i <= vol {
		return dir[:vol+1]
	}
	return dir[:i]
}

// split breaks a path into components on either separator, dropping empty
// ones.
func split(p string) []string {
	return strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == filepath.Separator })
}
