# RFP: pathguard — one judgement of whether a path may be touched

- Status: Accepted (2026-09-22) — revised after two independent design reviews and the operator's decisions of 2026-09-22 (two layers in one module; runtimes later; local versus outbound policies); implementation reviewed independently three times the same day, findings folded in (§ "What the implementation reviews changed")
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

// A place without an absolute path refuses every call (ErrBadPlace).
var ErrBadPlace error

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
    Home         string           // "" → $HOME, plus the account's home (os/user) when it differs; neither known → every call is refused
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
func (r Resolver) CheckBeneath(dir string) error                          // v0.2.0: <work_dir>/<workspace_id>

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
2. **Two comparisons, always both.** Identity is anchored. A place is its
   deepest existing ancestor-or-self plus the folded names below it. A form
   matches when one of its own existing ancestors is the same file
   (`os.SameFile`) and its remaining names begin with the place's. That catches
   every spelling of what exists — case, links, firmlinks, `/.nofollow`, `/.vol`,
   normalisation, a hard link to a file entry such as `~/.netrc` — for a place
   that exists and for one that does not yet. The name comparison, folded, covers
   a filesystem whose inode numbers cannot be trusted (macFUSE without `use_ino`,
   smbfs), where identity alone would be weaker than today. Names are folded the
   way APFS folds them — Unicode case folding with its one-to-many expansions,
   not ASCII lowercase. Folding over-refuses on a case-sensitive disk; a floor
   may.
3. **Exact places match the form itself, never its ancestors** — otherwise every
   temporary directory is refused.
4. **Home comes from the caller, by identity.** The runtimes' `homePrefixRe`
   (`/users/…`, `/home/…` by name) misses `/root`, `/private/var/root`, Windows
   homes and a moved `$HOME`, and matches other users' homes. `Floor(home)` with
   identity replaces it: the policies protect the home directory of the account
   the server runs as, and no longer another user's `.claude` as a name. With no
   home given, that is `$HOME` and, when it differs, the account's home from the
   user database: a server started with `HOME` pointing elsewhere still protects
   the real one.
5. **An unknown home refuses.** Today's check returns "" when the home directory
   cannot be found — it passes everything. `Floor("")` is an error, and so is a
   relative home (it would put the floor under the working directory); the
   resolver refuses every call, saying why.
6. **Cost is bounded per call, nothing is cached.** Per check, each place's own
   forms and their ancestors are looked up once, each credential directory is
   listed once for its links, and each form's ancestors are walked once, with
   work linear in the form's length apart from the stats themselves. A form
   longer than any system opens (4096 bytes; 32 KiB on Windows), whether given
   or produced by a link hop, refuses the path before anything is looked at.
   Measured on Apple Silicon: about 2 ms per check for a realistic path
   (`BenchmarkLocalCheck`) and for the longest the cap lets through
   (`BenchmarkLocalCheckLongestPath`), and about 0.33 s for the worst case —
   39 planted links, every form at the cap (`BenchmarkLocalCheckLinkChainAtTheCap`).
   Comparisons are done in memory. A cache would miss a place created after it
   was filled.
7. **Standard library only, pinned by a test.** Two consumers promise no
   third-party dependencies (reworded to allow nlink-jp modules, per the
   operator, 2026-09-22); the module keeps the promise with a test that its
   `go.mod` has no `require`.

### What the implementation reviews changed

Three independent reviews of the implementation, before release, found holes and
test gaps. Every fix is checked by a mutation that a test kills, except
`filepath.Abs` on Windows, which cannot run here.

**First review** — five holes:

- **Unicode folding.** `strings.ToLower` let `id_rſa` (U+017F) through; on APFS
  it opens `id_rsa`. Measured on the disk: `ſ` = `s`, the Kelvin sign = `k`,
  `ﬆ` and `ﬅ` = `st`, `ß` = `ss`. The comparison key is now the smallest member
  of each rune's simple-folding orbit, after the full foldings (`ß`, `ẞ`, the
  Latin ligatures). One test checks every table entry, and another the pairs
  it can create against the disk it runs on.
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
- **Windows** (six of the servers ship Windows binaries): a relative path is
  made absolute with `filepath.Abs`, since Windows applies `..` by name and has
  volume-relative forms; the name comparison drops the trailing dots and spaces
  Windows ignores.

**Second review** — one high and three medium holes. The high one and one medium
one had the same cause, so they were fixed at the cause rather than one by one:

- **A place that did not exist yet was compared only by its spelling**, and the
  spellings of its existing parent are unbounded. The firmlink,
  `/.nofollow/…`, `/.vol/<dev>/<ino>/…`, a linked `~/.config`, and a
  normalised non-ASCII home all created the real `~/.aws/credentials` or
  `~/.config/gem-agent/config.toml`. The cause was a place without an
  identity. The fix is the anchor (decision 2), which also covers the next
  point.
- **A dangling link inside a floor directory** (`~/.ssh/config` → a missing
  file in a sync folder) left its target unprotected, so the file could be
  planted there. The target is now an ordinary place, anchored like any
  missing one.
- **A place without `Why` protected nothing**, because every caller reads an
  empty sentence as "allowed". Empty words are now filled in, and a place
  without an absolute path is `ErrBadPlace` and refuses every call.
- **Windows normalisation** applied only to relative paths. Every path now goes
  through `filepath.Abs`. A stream suffix (`.env::$DATA`) and trailing dots and
  spaces are dropped from names for the name rules too, a rooted link target
  (`\Users\u`) is put on the link's drive, and junctions are followed as
  links.
- **Low findings, taken:**
  - A link to `/` or to the home directory inside a floor directory made
    everything a protected tree; such a target is now skipped, and a server's
    own directory's links are not followed.
  - A working directory that cannot be read now refuses a relative path.
  - The account's own home is protected when `$HOME` names another.

**Third review** — every earlier finding confirmed closed, and one medium hole:

- **One path argument could cost seconds.** `look` built each ancestor's
  remaining names by prepending, which is quadratic in the number of segments,
  and no length was capped. A 120 KB path cost 14.5 s per check. The names are
  now folded once and shared, and a path longer than any system opens is refused
  (decision 6).
- **Doc: the check-then-use race was not written down.** It is now an accepted
  limit.
- **Not taken:** a test seam over the Windows path branches (`absolute`,
  `joinTarget`). `filepath`'s Windows behaviour (`VolumeName`, `Abs`) exists
  only on Windows, so a seam on darwin would test a simulation, not the code.
  The docs say these branches are reasoned.

**Re-check of the third review's fix** — the cap covered only the given path:

- **A link hop could still grow a form without bound.** A 97-byte path through
  one link whose target was a 1 KB relative path produced 40 KB forms over 40
  hops and cost 21 s. `look` also still re-cleaned every prefix
  (`filepath.Dir`), which is quadratic. The cap now applies to every form in
  `forms`, and a path that does not resolve is refused before any form is
  looked at. `look` and `step` walk by substrings (`ancestors`, `parent`,
  `child`), pinned to `filepath`'s strings by a test.
- **Considered and rejected:** stopping `look` at the first ancestor that does
  not exist, which would have cut the worst case further. Existence is not
  monotone along a path: `/.vol/<dev>` does not stat while `/.vol/<dev>/<ino>`
  does (measured), so the anchor that closes the second review's `/.vol` hole
  would be lost.
- **Not taken:** capping the cleaned hop form rather than the raw one. The raw
  cap can refuse a legitimate path whose link target is over 4 KB yet cleans
  short. That is reachable only on Linux (on darwin a walked path and a link
  target are each at most 1024 bytes) and only with contrived targets, and it
  fails closed. Capping the raw string also bounds what `step` walks.


### What changes for the servers (each CHANGELOG says it)

- **Local (reads and writes): refused now** — the real places under your home
  from the combined list (`~/.kube`, `~/.config/gh`, `~/.azure`, `~/.terraform.d`,
  `~/.gemini`, `~/.config/mcp-bridge`, `~/.netrc`, `~/.npmrc`, `~/.pypirc`,
  `~/.git-credentials`, `~/.vault-token`, `~/.docker/config.json`, `~/.claude.json`,
  `~/.bash_history`, `~/.zsh_history`), and every spelling of any floor place,
  whether it exists yet or not. **Accepted now** — `.env.example`, `.env.sample`,
  `.env.template`, `.env.dist`.
- **Outbound (uploads): refused now** in addition — a credential directory or
  file name as a segment anywhere, and the secret names anywhere. slack-mcp-extender
  gains the `.env` rule (with `allow_hidden=true` it uploads a `.env` today); three
  of its containment tests will report `sensitive_path` for `.env` instead of a
  hidden component.
- **`work_dir`**: refused also when the home directory cannot be determined, and
  for Linux `/etc`.

### Accepted limits (the floor stays a floor)

- A verdict is a snapshot: a link created between the check and the open is not
  seen. Callers open the resolved path they checked and confine writes (`os.Root`,
  `O_NOFOLLOW`); the servers' own containment is where that race is closed.
- A hard link to a file inside a floor **directory** (e.g. `~/.ssh/id_rsa`) under
  another name, or a copy of a secret, is not detected by the Local policy.
- On a filesystem with unstable inode numbers, identity can collide and refuse a
  legitimate path; there the name comparison is what protects.
- Unicode normalisation is handled by identity. Only the names below a missing
  place's deepest existing directory are compared without it. That matters only
  for a protected directory with non-ASCII names that has not been created yet;
  the floor's names are ASCII.
- Only the links directly inside a credential or agent-control directory are
  followed to their targets; a link deeper inside protects its own location,
  not where it leads.
- Windows is reasoned from the platform's documented behaviour, not measured
  (there is no Windows machine to measure on). 8.3 short names reach an
  existing place by identity but pass the Outbound policy's name-only rules.
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
