# RFP: pathguard — one judgement of whether a path may be touched

- Status: Accepted (2026-09-22) — revised after two independent design reviews and the operator's decisions of 2026-09-22 (two layers in one module; runtimes later; local versus outbound policies); implementation reviewed independently the same day, findings folded in (§ "What the implementation review changed")
- Date: 2026-09-22
- Series: lib-series
- Amends (on acceptance): organization ADR-021 §4, §7 and §10, through a new organization ADR-022

## Problem

Two kinds of code decide whether a path may be touched, and both decide it by
name.

- **Nine MCP servers** implement organization ADR-021 — the caller's
  `work_dir`, a closed list of checks, and a floor of places no file argument may
  reach — each in its own `internal/workdir` (§10: "There is no shared Go module
  across these servers"). Eight copies differ only in import paths and ADR
  numbers; slack-mcp-extender's has drifted further: no `.env` rule, its own
  check order, system trees compared on every spelling, and no `Resolver` type
  (`Resolve(arg, meta, serverDirs)`).
- **gem-agent and lagent** keep a longer credential list (`internal/sandbox`),
  used by their file tools and to generate the Seatbelt profile.

The lists differ. The servers' floor is nine home directories and `.env`; the
runtimes' adds `.kube`, `.config/gh`, `.azure`, `.terraform.d`, `.gemini`,
`.config/mcp-bridge`, the files `.netrc`, `.npmrc`, `.pypirc`, `.git-credentials`,
`.vault-token`, `.docker/config.json`, `.claude.json`, `.bash_history`,
`.zsh_history`, and names that are secrets wherever they sit (`id_rsa` and the
other key names, `credentials.json`, `application_default_credentials.json`,
`*service-account*.json`).

The servers compare by name on a case-insensitive disk (APFS). Found on
chrome-pilot-mcp on 2026-09-22 and probed again in review: `.ENV` passes the
`.env` rule, `~/.SSH` the credential floor, `/USERS/<name>` the home check,
`/USR/local` the system trees, a case variant of a server's own directory
`work_dir_denied`. And one hole no comparison of end points closes: `~/.ssh` is
a real directory on the reviewing machine but `~/.ssh/config` links into a sync
folder, so `work/x → ~/.ssh/config` resolves somewhere that is not, and is not
under, `~/.ssh`. When the home directory cannot be determined, today's check
passes everything.

## What it is

One Go module, `github.com/nlink-jp/pathguard`, standard library only,
`go 1.23` (the oldest consumer's line), in two layers:

- **`pathguard`** — the general judgement: whether a path lies in a place
  (links resolved one hop at a time, compared by identity and by name), the one
  list of places, and two policies built from it. It knows nothing of MCP or
  work directories.
- **`pathguard/workdir`** — ADR-021 on top: `work_dir` from the argument or
  `_meta`, the error codes, existence and writability, the protected
  directories of the server using it.

Each server keeps a local `internal/workdir` package with the same import path,
so the code that **resolves** a work directory or checks a path does not change;
the few lines that **build** the resolver switch to the module's constructor
(eight servers write `Resolver{Denied: …}` today, and their tests `Resolver{}`).

### Two policies

The operator's decision (2026-09-22): what a server may **read or write on this
machine** and what it may **send off it** are judged differently, because a key
that has left the machine cannot be taken back, while a copy of one in an
incident-response collection is exactly what an analyst needs to read.

- **Local** — for reads and writes (data-toolbox, pcap-analyzer, the scribes,
  image-forge's inputs, every write). Refused: the real places under **your**
  home directory — the credential directories and files of the combined list,
  and the agent-control directories (`.config/gem-agent`, `.config/lagent`) —
  compared by identity and by name; and `.env` / `.env.*` anywhere except the
  committed templates. A file of the same name elsewhere — `evidence/home/bob/.bash_history`,
  a project's own `.npmrc`, an exported `service-accounts.json` — is read.
- **Outbound** — for a file that leaves the machine (slack-mcp-extender's uploads,
  chrome-pilot-mcp's `upload_file`). Local, plus the runtimes' rule: a
  credential directory or file name as a path segment anywhere (`.claude`,
  `.gemini`, `.codex` only under a home directory, since a project's `.claude/`
  holds its skills), and the secret names anywhere.

System trees and exact entries (`/`, `/private/var`, the home directory itself)
refuse a `work_dir`; they are not part of either file policy — reading
`/etc/hosts` is not a leak, and writes are confined to `work_dir` anyway.

### API sketch

```go
package pathguard

type Kind int // System, Credential, AgentControl, Protected

// Place is a location a path may not lie in: the directory and everything under
// it, or — Exact — only the path itself.
type Place struct {
    Path   string
    Exact  bool
    Kind   Kind
    Reason string // details.reason, e.g. "sensitive_path", "server_dir"
    Why    string // the sentence a refusal gives
}

// Forms returns every spelling of p worth checking: as given, every path on the
// way (p with its links replaced one hop at a time), and the final path.
func Forms(p string) []string

// ServerDir is the Place for a server's own directory (reason "server_dir").
func ServerDir(path, note string) Place

// Check reports the first place any form of any of paths lies in.
func Check(places []Place, paths ...string) (reason, why string)

// Floor builds the one list for a home directory. An empty or relative home is
// ErrNoHome: the caller refuses rather than checking nothing.
func Floor(home string) ([]Place, error)

// A Policy judges file paths; Local and Outbound are the two there are.
type Policy struct{ /* places, name rules */ }
func Local(home string, protected ...Place) (Policy, error)
func Outbound(home string, protected ...Place) (Policy, error)
func (p Policy) Check(paths ...string) (reason, why string)
```

```go
package workdir // github.com/nlink-jp/pathguard/workdir

const MetaKey = "jp.nlink/work_dir"
const (CodeRequired = "work_dir_required"; CodeInvalid = "work_dir_invalid";
       CodeNotFound = "work_dir_not_found"; CodeNotWritable = "work_dir_not_writable";
       CodeDenied = "work_dir_denied")

type Error struct{ Code, Message string; Details map[string]any }

type Options struct {
    Home         string           // "" → os.UserHomeDir(); if that fails, every call is refused
    Protected    []pathguard.Place // this server's own directories, and any it guards
    RequiredHint string           // what the directory is for here, appended to work_dir_required
}

// NewResolver is the only way to build a working Resolver; a zero Resolver
// refuses everything (a resolver built without its server's directories once
// shipped denying none of them).
func NewResolver(o Options) Resolver
func (r Resolver) Resolve(arg string, meta map[string]json.RawMessage) (string, error)
func (r Resolver) Validate(dir string) (string, error)
func (r Resolver) LocalPath(raw, resolved string) (reason, why string)    // Local + Protected
func (r Resolver) OutboundPath(raw, resolved string) (reason, why string) // Outbound + Protected

// For call sites that hold no Resolver (voice-scribe's transcribe today): the
// policy is built from this process's home; an unknown home refuses.
func Sensitive(paths ...string) string
func SensitiveOutbound(paths ...string) string
```

`work_dir_denied` carries `details` `{work_dir, resolved, reason}`.

The eight copies' messages are kept word for word; `RequiredHint` carries each
server's one sentence. slack-mcp-extender moves to the shared order and wording.

## Design decisions

1. **Every hop is a form.** Links are resolved one at a time (`Lstat`,
   `Readlink`); the path as given, every whole path on the way, and the final
   path are all checked — not a link's location alone when it is only a prefix
   (`/var` would otherwise match the exact place `/private/var` for every darwin
   temporary directory). `work/x → ~/.ssh/config → <sync>/config` is refused because
   its middle form lies in `~/.ssh`.
2. **Two comparisons, always both.** Identity (`os.SameFile` against the form and
   the existing directories above it) catches every spelling of an existing
   place — case, links, normalisation, a hard link to a file entry such as
   `~/.netrc`. The name comparison, case-folded, covers a place that does not
   exist yet and a filesystem whose inode numbers cannot be trusted (macFUSE
   without `use_ino`, smbfs), where identity alone would be weaker than today.
   Names are folded the way APFS folds them — Unicode case folding with its
   one-to-many expansions, not ASCII lowercase. Folding over-refuses on a
   case-sensitive disk; a floor may.
3. **Exact places match the form itself, never its ancestors** — otherwise every
   temporary directory is refused.
4. **Home comes from the caller, by identity.** The runtimes' `homePrefixRe`
   (`/users/…`, `/home/…` by name) misses `/root`, `/private/var/root`, Windows
   homes and a moved `$HOME`, and matches other users' homes. `Floor(home)` with
   identity replaces it: the policies protect the home directory of the account
   the server runs as, and no longer another user's `.claude` as a name.
5. **An unknown home refuses.** Today's check returns "" when the home directory
   cannot be found — it passes everything. `Floor("")` is an error, and so is a
   relative home (it would put the floor under the working directory); the
   resolver refuses every call, saying why.
6. **Cost is bounded per call, nothing is cached.** Each place is `Stat`ed once,
   each form's ancestors walked once, comparisons done in memory. A cache would
   miss a place created after it was filled.
7. **Standard library only, pinned by a test.** Two consumers promise no
   third-party dependencies (reworded to allow nlink-jp modules, per the
   operator, 2026-09-22); the module keeps the promise with a test that its
   `go.mod` has no `require`.

### What the implementation review changed

An independent review of the implementation, before release, found five holes
and a set of test gaps. Each fix has a test that fails without it.

- **Unicode folding.** `strings.ToLower` let `id_rſa` (U+017F) through; on APFS
  it opens `id_rsa`. Measured on the disk: `ſ` = `s`, the Kelvin sign = `k`,
  `ﬆ` and `ﬅ` = `st`, `ß` = `ss`. The comparison key is now the smallest member
  of each rune's simple-folding orbit, after the full foldings (`ß`, `ẞ`, the
  Latin ligatures); a test checks every pair against the disk it runs on.
- **Link targets.** A link's own location inside a place was protected, its
  target was not: the sync folder's copy of `~/.ssh/config` could be read by
  naming it. The targets of the links directly inside a non-system directory
  place are now places too — one level, a stated limit.
- **`..` after a missing component** (`work/missing/../link`) was joined by
  name, and a link in the part that exists was not followed. The cleaned
  remainder is now resolved again.
- **A relative home** built a floor under the working directory. It is now
  `ErrNoHome`.
- **A call site without a `Resolver`** (voice-scribe's transcribe calls
  `workdir.Sensitive`) had no module function to move to; `Sensitive` and
  `SensitiveOutbound` fail closed on an unknown home.
- **Windows** (the servers also build there): a relative path is made absolute
  with `filepath.Abs`, since Windows applies `..` by name and has
  volume-relative forms; the name comparison drops the trailing dots and spaces
  Windows ignores.

### What changes for the servers (each CHANGELOG says it)

- **Local (reads and writes): refused now** — the real places under your home
  from the combined list (`~/.kube`, `~/.config/gh`, `~/.azure`, `~/.terraform.d`,
  `~/.gemini`, `~/.config/mcp-bridge`, `~/.netrc`, `~/.npmrc`, `~/.pypirc`,
  `~/.git-credentials`, `~/.vault-token`, `~/.docker/config.json`, `~/.claude.json`,
  `~/.bash_history`, `~/.zsh_history`), and every case variant and link route to
  any floor place. **Accepted now** — `.env.example`, `.env.sample`,
  `.env.template`, `.env.dist`.
- **Outbound (uploads): refused now** in addition — a credential directory or
  file name as a segment anywhere, and the secret names anywhere. slack-mcp-extender
  gains the `.env` rule (with `allow_hidden=true` it uploads a `.env` today); three
  of its containment tests will report `sensitive_path` for `.env` instead of a
  hidden component.
- **`work_dir`**: refused also when the home directory cannot be determined, and
  for Linux `/etc`.

### Accepted limits (the floor stays a floor)

- A hard link to a file inside a floor **directory** (e.g. `~/.ssh/id_rsa`) under
  another name, or a copy of a secret, is not detected by the Local policy.
- On a filesystem with unstable inode numbers, identity can collide and refuse a
  legitimate path; there the name comparison is what protects.
- Unicode normalisation is handled for existing places (by identity), not for a
  place that does not exist yet under a non-ASCII home.
- Only the links directly inside a place are followed to their targets; a
  link deeper inside protects its own location, not where it leads.
- Another user's `.claude`, `.gemini` and `.codex` are not protected (decision
  4); the fixture records this as an intended difference from the runtimes.

### Keeping the runtimes' list and this one together

Until the runtimes adopt the module, their list stays authoritative for them.
The module cannot import their `internal` packages, so:

- `testdata/runtime-lists.json` holds the runtimes' four lists
  (`credentialDirs`, `homeOnlyDirs`, `credentialFiles`, `credentialNames`), and a
  table of paths with the verdict the Outbound policy must give (segment,
  home-only and template cases) — verdicts, because a regex cannot be "contained"
  in a list of places;
- a `check-org.sh` check parses both runtimes' `lane.go` with `go/ast` and fails
  when either differs from the fixture — the only place that sees all three
  repositories.

### Out of scope, and next

- The servers' own containment (chrome-pilot-mcp's `confine.go`,
  slack-mcp-extender's `containment`) stays in the servers and calls
  `LocalPath` / `OutboundPath`; chrome-pilot passes its guarded browser profiles
  as protected places and drops its second identity layer.
- **gem-agent and lagent adopt `pathguard` in a later, separate step** (operator,
  2026-09-22): the list also generates their Seatbelt profile. Whether the
  Seatbelt name rules fold case is unmeasured and belongs to that step.
- A sandboxing proxy (ADR-021 §7's deferred work).

## Development plan

1. **Module** in `_wip/pathguard`: both layers; both server copies' tests ported;
   new tests for case variants (skipped, with a message, on a case-sensitive
   disk), a linked file inside a real floor directory, exact versus subtree
   places, an unknown home, a zero `Resolver`, the templates, Local versus
   Outbound on the same paths, the runtime fixture's verdict table, and the
   no-`require` pin. Independent review. Release v0.1.0 as `nlink-jp/pathguard`,
   submodule in lib-series.
2. **Servers**, one release each: voice-scribe first (the old transplant source),
   then gem-scribe, image-forge (its `dist` rebuilt, then image-forge-gui, which
   bundles it), voice-studio-mcp, video-studio-mcp, data-toolbox-mcp,
   pcap-analyzer-mcp, chrome-pilot-mcp, slack-mcp-extender.
3. **Organization**: ADR-022 amending ADR-021 §4 (the order the servers actually
   apply — denied before writable), §7 (the combined list, the templates, the two
   policies) and §10 (the judgement lives in one module; transplanting is
   retired); CONVENTIONS and knowledge updated; `check-org.sh` checks — no floor
   literal in a non-test Go file of a consuming server, every consumer on the
   module's latest tag, and the runtime fixture matching both `lane.go`.
4. **Runtimes** (separate, later): gem-agent and lagent take their list and path
   judgement from `pathguard`, with the Seatbelt profile generated from it.
