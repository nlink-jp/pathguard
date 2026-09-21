package workdir

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nlink-jp/pathguard"
)

// Ported from voice-scribe's and slack-mcp-extender's copies, which this
// package replaces, plus the cases those copies got wrong.

func realTemp(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func code(t *testing.T, err error) string {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("error %v is not a work_dir Error", err)
	}
	return e.Code
}

func meta(t *testing.T, v any) map[string]json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]json.RawMessage{MetaKey: raw}
}

// resolver builds a Resolver whose home is a directory the test owns.
func resolver(t *testing.T, protected ...pathguard.Place) (Resolver, string) {
	t.Helper()
	home := realTemp(t)
	return NewResolver(Options{Home: home, Protected: protected, RequiredHint: "Results come back as paths."}), home
}

func caseInsensitive(t *testing.T, dir string) bool {
	t.Helper()
	probe := filepath.Join(dir, "case-probe")
	if err := os.Mkdir(probe, 0o755); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(probe) }()
	_, err := os.Stat(filepath.Join(dir, "CASE-PROBE"))
	return err == nil
}

// The argument is the caller's own statement of where it can read files back;
// a runtime hint is a default beneath it.
func TestResolveArgumentWins(t *testing.T) {
	r, _ := resolver(t)
	arg, hint := realTemp(t), realTemp(t)
	got, err := r.Resolve(arg, meta(t, hint))
	if err != nil || got != arg {
		t.Errorf("Resolve = %q, %v; want %q", got, err, arg)
	}
}

func TestResolveFallsBackToRequestMeta(t *testing.T) {
	r, _ := resolver(t)
	hint := realTemp(t)
	got, err := r.Resolve("", meta(t, hint))
	if err != nil || got != hint {
		t.Errorf("Resolve = %q, %v; want %q", got, err, hint)
	}
}

// There is no server-owned default to fall through to: a destination the
// caller cannot read back turns a successful call into a path to nothing. The
// message says what to pass, and the server's own sentence.
func TestResolveWithoutEitherChannelIsAnError(t *testing.T) {
	r, _ := resolver(t)
	_, err := r.Resolve("", nil)
	if code(t, err) != CodeRequired {
		t.Fatalf("code = %q", code(t, err))
	}
	if !strings.Contains(err.Error(), "read back") || !strings.HasSuffix(err.Error(), "Results come back as paths.") {
		t.Errorf("message = %q", err.Error())
	}
}

func TestResolveRejectsNonStringMeta(t *testing.T) {
	r, _ := resolver(t)
	if _, err := r.Resolve("", meta(t, 42)); code(t, err) != CodeInvalid {
		t.Errorf("code = %q, want %q", code(t, err), CodeInvalid)
	}
}

func TestValidate(t *testing.T) {
	r, home := resolver(t)
	dir := realTemp(t)
	file := filepath.Join(dir, "a-file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	ssh := filepath.Join(home, ".ssh", "keys")
	if err := os.MkdirAll(ssh, 0o700); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ name, in, want string }{
		{"tilde", "~/work", CodeInvalid},
		{"relative", "work/dir", CodeInvalid},
		{"parent segment", dir + "/../elsewhere", CodeInvalid},
		{"missing", filepath.Join(dir, "not-there"), CodeNotFound},
		{"a file", file, CodeNotFound},
		{"system tree", "/usr/bin", CodeDenied},
		{"filesystem root", "/", CodeDenied},
		{"home itself", home, CodeDenied},
		{"a credential directory", ssh, CodeDenied},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := r.Validate(c.in)
			if err == nil {
				t.Fatalf("Validate(%q) succeeded", c.in)
			}
			if got := code(t, err); got != c.want {
				t.Errorf("code = %q, want %q (%v)", got, c.want, err)
			}
		})
	}
}

// On darwin a per-user temporary directory resolves under /private/var, and
// Codex names exactly that as a place it can write: an exact entry must not
// refuse what lies below it.
func TestValidateAcceptsATemporaryDirectory(t *testing.T) {
	r, _ := resolver(t)
	dir := t.TempDir()
	got, err := r.Validate(dir)
	if err != nil {
		t.Fatalf("Validate(%q): %v", dir, err)
	}
	if want, _ := filepath.EvalSymlinks(dir); got != want {
		t.Errorf("Validate = %q, want the resolved %q", got, want)
	}
}

func TestValidateRefusesServerOwnedDirectoriesNotTheirSiblings(t *testing.T) {
	own := filepath.Join(realTemp(t), "server")
	inside := filepath.Join(own, "workspaces")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	r, _ := resolver(t, pathguard.ServerDir(own, "which holds its OAuth tokens"))
	for _, dir := range []string{own, inside} {
		_, err := r.Validate(dir)
		if code(t, err) != CodeDenied || !strings.Contains(err.Error(), "which holds its OAuth tokens") {
			t.Errorf("Validate(%q) = %v, want work_dir_denied naming what it holds", dir, err)
		}
	}
	sibling := own + "-elsewhere"
	if err := os.Mkdir(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Validate(sibling); err != nil {
		t.Errorf("Validate(%q) = %v, want accepted", sibling, err)
	}
}

func TestValidateRefusesAServerDirectoryThroughALink(t *testing.T) {
	own := filepath.Join(realTemp(t), "server")
	if err := os.MkdirAll(own, 0o755); err != nil {
		t.Fatal(err)
	}
	door := filepath.Join(realTemp(t), "door")
	if err := os.Symlink(own, door); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	r, _ := resolver(t, pathguard.ServerDir(own, ""))
	if _, err := r.Validate(door); code(t, err) != CodeDenied {
		t.Errorf("Validate(link to server dir) = %v", err)
	}
}

// The copies compared names, and this disk folds case: every one of these
// passed them.
func TestValidateRefusesEverySpellingOfARefusedPlace(t *testing.T) {
	own := filepath.Join(realTemp(t), "chrome-pilot-mcp")
	if err := os.MkdirAll(filepath.Join(own, "profiles"), 0o755); err != nil {
		t.Fatal(err)
	}
	r, home := resolver(t, pathguard.ServerDir(own, ""))
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if !caseInsensitive(t, home) {
		t.Skip("case-sensitive filesystem: another spelling is another directory")
	}
	for _, dir := range []string{
		filepath.Join(filepath.Dir(own), "CHROME-PILOT-MCP", "profiles"),
		filepath.Join(home, ".SSH"),
		strings.ToUpper(home),
	} {
		if _, err := r.Validate(dir); code(t, err) != CodeDenied {
			t.Errorf("Validate(%q) = %v, want work_dir_denied", dir, err)
		}
	}
}

func TestValidateRejectsAnUnwritableDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write anywhere")
	}
	r, _ := resolver(t)
	dir := filepath.Join(realTemp(t), "read-only")
	if err := os.Mkdir(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Validate(dir); code(t, err) != CodeNotWritable {
		t.Errorf("Validate = %v, want work_dir_not_writable", err)
	}
}

// A resolver built without NewResolver refuses everything: one built without
// its server's directories once shipped denying none of them.
func TestAZeroResolverRefusesEverything(t *testing.T) {
	var r Resolver
	if _, err := r.Validate(realTemp(t)); code(t, err) != CodeDenied {
		t.Errorf("zero Resolver Validate = %v", err)
	}
	if reason, _ := r.LocalPath("/srv/data/x.csv", "/srv/data/x.csv"); reason != "unconfigured" {
		t.Errorf("zero Resolver LocalPath reason = %q", reason)
	}
}

// Without a home directory the floor cannot be applied; the copies passed
// everything then. Now every call is refused, saying why.
func TestAnUnknownHomeRefusesEveryCall(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	t.Setenv("home", "")
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		t.Skipf("the home directory is still known here: %q", h)
	}
	r := NewResolver(Options{})
	_, err := r.Validate(realTemp(t))
	if code(t, err) != CodeDenied || !strings.Contains(err.Error(), "home directory") {
		t.Errorf("Validate = %v", err)
	}
	if reason, _ := r.OutboundPath("/srv/x", "/srv/x"); reason != "home_unknown" {
		t.Errorf("OutboundPath reason = %q", reason)
	}
}

func TestTheDenialCarriesDetails(t *testing.T) {
	r, _ := resolver(t)
	_, err := r.Validate("/usr/bin")
	var e *Error
	if !errors.As(err, &e) || e.Details["work_dir"] != "/usr/bin" || e.Details["resolved"] == nil {
		t.Errorf("details = %#v", e)
	}
}

// Local reads a copy in an evidence collection; Outbound does not send it.
func TestLocalAndOutboundPathsDiffer(t *testing.T) {
	r, home := resolver(t)
	evidence := filepath.Join(realTemp(t), "evidence", "home", "bob", ".ssh", "authorized_keys")
	if err := os.MkdirAll(filepath.Dir(evidence), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(evidence, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, why := r.LocalPath(evidence, evidence); why != "" {
		t.Errorf("LocalPath refused the evidence copy: %s", why)
	}
	if _, why := r.OutboundPath(evidence, evidence); why == "" {
		t.Error("OutboundPath would send the evidence copy")
	}
	real := filepath.Join(home, ".ssh", "id_rsa")
	if _, why := r.LocalPath(real, real); why == "" {
		t.Error("LocalPath passed the real ~/.ssh/id_rsa")
	}
}
