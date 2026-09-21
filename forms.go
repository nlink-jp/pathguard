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

// forms is Forms with a verdict on whether the chain of links ended. A chain
// longer than maxHops, or a link that cannot be read, is ok=false, and the
// caller refuses rather than guess where the path leads.
func forms(p string) (out []string, ok bool) {
	if p == "" {
		return nil, true
	}
	seen := map[string]bool{}
	add := func(s string) {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	cur := absolute(p)
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
		add(filepath.Clean(r.next))
		cur = r.next
	}
	return out, false
}

// absolute joins a relative path onto the working directory without cleaning
// it, so a ".." in it is resolved against real directories by step, not
// cancelled by name. Windows applies ".." by name and has volume-relative
// forms (\Users, C:foo), so there filepath.Abs is what the system does.
func absolute(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	if runtime.GOOS == "windows" {
		if a, err := filepath.Abs(p); err == nil {
			return a
		}
		return p
	}
	wd, err := os.Getwd()
	if err != nil {
		return p
	}
	return wd + string(filepath.Separator) + p
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
// rest is joined by name.
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
			walked = filepath.Dir(walked)
			continue
		}
		cand := filepath.Join(walked, comp)
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
		if fi.Mode()&os.ModeSymlink == 0 {
			walked = cand
			continue
		}
		target, err := os.Readlink(cand)
		if err != nil {
			return stepResult{}
		}
		next := target
		if !filepath.IsAbs(target) {
			next = walked + sep + target
		}
		if rest := strings.Join(parts[i+1:], sep); rest != "" {
			next += sep + rest
		}
		return stepResult{link: cand, next: next, ok: true}
	}
	return stepResult{final: filepath.Clean(walked), ok: true}
}

// split breaks a path into components on either separator, dropping empty
// ones.
func split(p string) []string {
	return strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == filepath.Separator })
}
