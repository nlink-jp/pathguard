package pathguard

import (
	"os"
	"path/filepath"
	"testing"
)

// Where is where a walk ends. Forms de-duplicates its spellings, so when a
// chain of links comes back to a spelling it already produced, the end is an
// earlier element and the last one is a middle hop; Where returns the end.
func TestWhereIsTheEndOfTheWalk(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	w, q := filepath.Join(root, "w"), filepath.Join(root, "q")
	for _, d := range []string{filepath.Join(w, "z"), q} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(w, "x.m4a"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(q, "L2"), filepath.Join(w, "L1")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(filepath.Join(w, "z"), filepath.Join(q, "L2")); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(w, "L1") + string(filepath.Separator) + ".." + string(filepath.Separator) + "x.m4a"
	want, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := Where(p)
	if !ok || got != want {
		t.Errorf("Where(%s) = %q, %v; want %q (EvalSymlinks)", p, got, ok, want)
	}

	// A dangling link is followed to its target; a path that exists ends
	// where EvalSymlinks does; a chain that does not end is not placed.
	if err := os.Symlink(filepath.Join(root, "gone", "t"), filepath.Join(w, "dangling")); err != nil {
		t.Fatal(err)
	}
	if got, ok := Where(filepath.Join(w, "dangling", "y")); !ok || got != filepath.Join(root, "gone", "t", "y") {
		t.Errorf("Where(through a dangling link) = %q, %v", got, ok)
	}
	if err := os.Symlink("loopB", filepath.Join(w, "loopA")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("loopA", filepath.Join(w, "loopB")); err != nil {
		t.Fatal(err)
	}
	if _, ok := Where(filepath.Join(w, "loopA")); ok {
		t.Error("a chain of links that does not end was placed")
	}
	if _, ok := Where(""); ok {
		t.Error("an empty path was placed")
	}
}
