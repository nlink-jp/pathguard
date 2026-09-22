package pathguard

import (
	"path/filepath"
	"runtime"
	"strings"
	"unicode"
)

// A case-insensitive filesystem decides equality by Unicode case folding, not
// by ASCII lowercase. Measured on APFS (2026-09-22): "ſ" (U+017F) names "s",
// the Kelvin sign names "k", and the ligatures "ﬆ" and "ﬅ" name "st" — so
// id_rſa opens id_rsa, and a rule written with strings.ToLower or
// strings.EqualFold (which never expands a ligature, and compares byte
// lengths when sliced) lets it through.
//
// fold is the comparison key: every rune replaced by the smallest member of
// its unicode.SimpleFold orbit (ſ, s and S become one rune; so do the Kelvin
// sign, k and K), after the full foldings that expand one rune into several —
// ß and ẞ to "ss", the Latin ligatures to their letters. Only equality of keys
// means anything.
//
// It is APFS's equivalence where the result is ASCII — and every protected
// name is ASCII. The second review measured every code point on APFS: the
// only characters it equates with ASCII letters are ſ, the Kelvin sign and
// the fullFolds entries. It does not normalise (composed and decomposed é
// differ here); identity covers normalisation for whatever exists.
var fullFolds = map[rune]string{
	'ß': "ss", 'ẞ': "ss",
	'ﬀ': "ff", 'ﬁ': "fi", 'ﬂ': "fl", 'ﬃ': "ffi", 'ﬄ': "ffl", 'ﬅ': "st", 'ﬆ': "st",
}

func fold(s string) string {
	var b strings.Builder
	for _, r := range s {
		if x, ok := fullFolds[r]; ok {
			for _, xr := range x {
				b.WriteRune(canon(xr))
			}
			continue
		}
		b.WriteRune(canon(r))
	}
	return b.String()
}

// canon is the smallest rune of r's simple case-folding orbit.
func canon(r rune) rune {
	m := r
	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		if f < m {
			m = f
		}
	}
	return m
}

// windowsNames switches on the name normalisation Windows applies (see
// windowsName). It is the platform's, and a variable so that the rules can be
// tested on any platform.
var windowsNames = runtime.GOOS == "windows"

// segKey is the comparison key of one path segment: fold, after, on Windows,
// windowsName.
func segKey(s string) string {
	if windowsNames {
		s = windowsName(s)
	}
	return fold(s)
}

// windowsName is a name as Windows opens it: without an alternate data stream
// (".env::$DATA" is the file .env, ".ssh::$INDEX_ALLOCATION" the directory
// .ssh) and without the trailing dots and spaces Windows ignores. It is
// applied to every segment, where Windows trims only the last; that can only
// refuse more. A drive ("C:") becomes "C" on both sides of a comparison alike.
func windowsName(s string) string {
	if i := strings.IndexByte(s, ':'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimRight(s, ". ")
}

// foldPath is segKey applied segment by segment, with "/" as the separator.
func foldPath(p string) string {
	segs := split(p)
	for i, s := range segs {
		segs[i] = segKey(s)
	}
	out := strings.Join(segs, "/")
	if filepath.IsAbs(p) || strings.HasPrefix(p, "/") {
		out = "/" + out
	}
	return out
}

// sameName reports whether two paths name the same place to a
// case-insensitive filesystem.
func sameName(a, b string) bool { return foldPath(a) == foldPath(b) }

// withinFold reports whether path is root or lies under it, compared by
// folded segments: a sibling that merely shares a prefix (/data-evil against
// /data) is not inside.
func withinFold(path, root string) bool { return withinFolded(foldPath(path), foldPath(root)) }

// withinFolded is withinFold on paths already folded.
func withinFolded(fp, fr string) bool {
	return fp == fr || strings.HasPrefix(fp, strings.TrimSuffix(fr, "/")+"/")
}
