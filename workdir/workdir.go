// Package workdir implements organization ADR-021, the work-directory contract
// of the fleet's file-mediated MCP servers, on top of pathguard: the caller's
// work_dir, from the tool argument or the request's _meta, validated by a
// closed list of checks, plus the two file policies for the paths a call
// names.
//
// The value is per session and per calling runtime, so a server cannot know
// it; only the caller can. There is deliberately no default: a server-chosen
// directory is readable by the caller only by coincidence, and when it is not,
// the call still succeeds and returns a path to a file the caller cannot open.
//
// It knows no protocol. A server keeps a small adapter that passes the
// request's _meta map in and maps Error onto its own structured errors.
package workdir

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/nlink-jp/pathguard"
)

// MetaKey is the request-level _meta key a runtime sets on every tools/call to
// name the session work directory.
const MetaKey = "jp.nlink/work_dir"

// Error codes, shared by the whole fleet so a caller sees one vocabulary.
const (
	CodeRequired    = "work_dir_required"
	CodeInvalid     = "work_dir_invalid"
	CodeNotFound    = "work_dir_not_found"
	CodeNotWritable = "work_dir_not_writable"
	CodeDenied      = "work_dir_denied"
)

// Error is a refusal carrying the code the caller branches on.
type Error struct {
	Code    string
	Message string
	Details map[string]any
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func newErr(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Options configure a Resolver.
type Options struct {
	// Home is the home directory the credential floor is relative to. Empty
	// means the process's: os.UserHomeDir(), and also the account's own home
	// from the user database when the environment names another. If neither
	// is known, every call is refused.
	Home string
	// Protected are the server's own configuration and state directories
	// (pathguard.ServerDir), and any other directory it guards.
	Protected []pathguard.Place
	// RequiredHint is the server's one sentence about what the directory is
	// for, appended to the work_dir_required message.
	RequiredHint string
}

// Resolver resolves and validates work directories and judges file paths. Only
// NewResolver builds a working one: the zero Resolver refuses everything,
// because a resolver built without its server's directories once shipped
// denying none of them.
type Resolver struct {
	built    bool
	setupErr error             // ErrNoHome or ErrBadPlace: every call is refused
	places   []pathguard.Place // the whole floor and the protected directories: for a work directory
	local    pathguard.Policy
	outbound pathguard.Policy
	hint     string
}

// NewResolver builds a Resolver.
func NewResolver(o Options) Resolver {
	r := Resolver{built: true, hint: strings.TrimSpace(o.RequiredHint)}
	home, extra, err := homes(o.Home)
	if err != nil {
		r.setupErr = err
		return r
	}
	floor, err := pathguard.Floor(home)
	if err != nil {
		r.setupErr = err
		return r
	}
	protected := append(nonSystem(extra), o.Protected...)
	if r.local, err = pathguard.Local(home, protected...); err != nil {
		r.setupErr = err
		return r
	}
	if r.outbound, err = pathguard.Outbound(home, protected...); err != nil {
		r.setupErr = err
		return r
	}
	r.places = append(append(floor, extra...), o.Protected...)
	return r
}

// accountHome is the home directory of the account this process runs as, from
// the user database rather than the environment; a test replaces it.
var accountHome = func() string {
	u, err := user.Current()
	if err != nil {
		return ""
	}
	return u.HomeDir
}

// homes returns the home directory the floor is relative to, and the floor of
// a second one: the account's own, when the environment names another. A
// server started with HOME pointing elsewhere still protects the account's
// real ~/.ssh. A home named by the caller is the only one.
func homes(given string) (home string, extra []pathguard.Place, err error) {
	if given != "" {
		return given, nil, nil
	}
	env, _ := os.UserHomeDir()
	acct := accountHome()
	if !filepath.IsAbs(env) {
		env = ""
	}
	if !filepath.IsAbs(acct) {
		acct = ""
	}
	switch {
	case env == "" && acct == "":
		return "", nil, pathguard.ErrNoHome
	case env == "":
		return acct, nil, nil
	case acct == "" || sameDir(env, acct):
		return env, nil, nil
	}
	extra, err = pathguard.Floor(acct)
	return env, extra, err
}

func sameDir(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	fa, errA := os.Stat(a)
	fb, errB := os.Stat(b)
	return errA == nil && errB == nil && os.SameFile(fa, fb)
}

// nonSystem drops the system places, which refuse a work directory but not a
// file (pathguard.Local and Outbound leave them out of their own floor).
func nonSystem(places []pathguard.Place) []pathguard.Place {
	var out []pathguard.Place
	for _, p := range places {
		if p.Kind != pathguard.System {
			out = append(out, p)
		}
	}
	return out
}

// setupReason is the details.reason of a Resolver that could not be built.
func setupReason(err error) string {
	if errors.Is(err, pathguard.ErrNoHome) {
		return "home_unknown"
	}
	return "unconfigured"
}

// Resolve returns the validated work directory for one call: the tool's
// work_dir argument, else the runtime's hint in meta[MetaKey], else an error.
// The returned path is absolute and symlink-resolved.
func (r Resolver) Resolve(arg string, meta map[string]json.RawMessage) (string, error) {
	dir := strings.TrimSpace(arg)
	if dir == "" {
		hint, err := metaHint(meta)
		if err != nil {
			return "", err
		}
		dir = hint
	}
	if dir == "" {
		msg := "work_dir is required: pass the absolute path of a directory you can read back " +
			"(your session or working directory)."
		if r.hint != "" {
			msg += " " + r.hint
		}
		return "", &Error{Code: CodeRequired, Message: msg}
	}
	return r.Validate(dir)
}

// Validate applies the closed list of checks, in order, and returns the
// resolved path: no ~, absolute, no .. segment; it exists and is a directory
// (it is not created — a typo must fail loudly); it is not a refused place; it
// is writable.
func (r Resolver) Validate(dir string) (string, error) {
	if strings.HasPrefix(dir, "~") {
		return "", newErr(CodeInvalid, "work_dir %q starts with ~: nothing expands it on this path — pass the absolute path", dir)
	}
	if !filepath.IsAbs(dir) {
		return "", newErr(CodeInvalid, "work_dir %q must be an absolute path", dir)
	}
	for _, seg := range strings.Split(filepath.ToSlash(dir), "/") {
		if seg == ".." {
			return "", newErr(CodeInvalid, "work_dir %q contains a .. segment; pass the path you mean", dir)
		}
	}
	resolved, err := filepath.EvalSymlinks(filepath.Clean(dir))
	if err != nil {
		return "", newErr(CodeNotFound,
			"work_dir %q does not exist — it is your directory, so this is a typo, not something to create here", dir)
	}
	fi, err := os.Stat(resolved)
	if err != nil {
		return "", newErr(CodeNotFound, "work_dir %q: %v", dir, err)
	}
	if !fi.IsDir() {
		return "", newErr(CodeNotFound, "work_dir %q is not a directory", dir)
	}
	if reason, why := r.denied(dir, resolved); why != "" {
		e := newErr(CodeDenied, "work_dir %q is refused: %s", dir, why)
		e.Details = map[string]any{"work_dir": dir, "resolved": resolved, "reason": reason}
		return "", e
	}
	if err := writable(resolved); err != nil {
		return "", newErr(CodeNotWritable, "work_dir %q is not writable by this server", dir)
	}
	return resolved, nil
}

func (r Resolver) denied(dir, resolved string) (reason, why string) {
	if !r.built {
		return "unconfigured", "this server's work-directory check was not set up (workdir.NewResolver)"
	}
	if r.setupErr != nil {
		return setupReason(r.setupErr), r.setupErr.Error()
	}
	if reason, why := pathguard.Check(r.places, dir, resolved); why != "" {
		return reason, why
	}
	if pathguard.EnvFile(filepath.Base(dir)) || pathguard.EnvFile(filepath.Base(resolved)) {
		return "sensitive_path", "a .env file holds credentials"
	}
	return "", ""
}

// CheckBeneath reports why dir, a directory beneath a validated work
// directory, may not be used — a workspace <work_dir>/<workspace_id>,
// typically, which may not exist yet — or nil. It applies the places that
// refuse a work directory to the directory actually used: work_dir=~/.config
// with workspace_id=gh is ~/.config/gh, and validating work_dir alone passed
// it. The error is work_dir_denied, with details {path, reason}.
func (r Resolver) CheckBeneath(dir string) error {
	if reason, why := r.denied(dir, dir); why != "" {
		e := newErr(CodeDenied, "%q is refused: %s", dir, why)
		e.Details = map[string]any{"path": dir, "reason": reason}
		return e
	}
	return nil
}

// LocalPath reports why a file a call names may not be read or written on this
// machine (pathguard.Local plus the protected directories), or two empty
// strings. Pass the path as the caller gave it and its resolved form; for a
// file that does not exist yet, pass the same value twice.
func (r Resolver) LocalPath(raw, resolved string) (reason, why string) {
	if reason, why := r.unusable(); why != "" {
		return reason, why
	}
	return r.local.Check(raw, resolved)
}

// OutboundPath reports why a file may not be sent off the machine
// (pathguard.Outbound plus the protected directories), or two empty strings.
func (r Resolver) OutboundPath(raw, resolved string) (reason, why string) {
	if reason, why := r.unusable(); why != "" {
		return reason, why
	}
	return r.outbound.Check(raw, resolved)
}

func (r Resolver) unusable() (reason, why string) {
	if !r.built {
		return "unconfigured", "this server's path check was not set up (workdir.NewResolver)"
	}
	if r.setupErr != nil {
		return setupReason(r.setupErr), r.setupErr.Error()
	}
	return "", ""
}

// Sensitive reports why a path may not be read or written on this machine —
// the Local policy with this process's home directories (as Options.Home
// empty) — or "". It is for the call sites that hold no Resolver; an unknown
// home refuses, with the reason.
// Pass every spelling you have: as given, and resolved.
func Sensitive(paths ...string) string {
	p, err := policyFor(pathguard.Local)
	if err != nil {
		return err.Error()
	}
	_, why := p.Check(paths...)
	return why
}

// SensitiveOutbound is Sensitive for a file that leaves the machine: the
// Outbound policy.
func SensitiveOutbound(paths ...string) string {
	p, err := policyFor(pathguard.Outbound)
	if err != nil {
		return err.Error()
	}
	_, why := p.Check(paths...)
	return why
}

func policyFor(build func(string, ...pathguard.Place) (pathguard.Policy, error)) (pathguard.Policy, error) {
	home, extra, err := homes("")
	if err != nil {
		return pathguard.Policy{}, err
	}
	return build(home, nonSystem(extra)...)
}

// writable reports whether this process can create an entry in dir. It is a
// create-and-remove rather than an access(2) call: syscall.Access does not
// exist on Windows, where these servers also build, and doing what the server
// is about to do is the more honest test.
func writable(dir string) error {
	f, err := os.CreateTemp(dir, ".work_dir-check-*")
	if err != nil {
		return err
	}
	name := f.Name()
	_ = f.Close()
	return os.Remove(name)
}

// metaHint reads the work directory a runtime attached to the request. A present
// but non-string value is an error rather than a silent miss: a runtime that sets
// the key wrongly should hear about it once, not have every call fall through to
// "work_dir is required".
func metaHint(meta map[string]json.RawMessage) (string, error) {
	raw, ok := meta[MetaKey]
	if !ok {
		return "", nil
	}
	var dir string
	if err := json.Unmarshal(raw, &dir); err != nil {
		return "", newErr(CodeInvalid, "request _meta[%q] is not a string", MetaKey)
	}
	return strings.TrimSpace(dir), nil
}
