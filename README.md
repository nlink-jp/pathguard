# pathguard

One judgement of whether a path may be touched, for [nlink-jp](https://github.com/nlink-jp) tools.

A Go library, standard library only. It decides whether a path lies in a place
nothing may reach — system locations, credential stores, an agent's
configuration, a server's own directories — and does so by **file identity as
well as by name**, so a case variant on a case-insensitive disk, a symlink, or a
firmlink cannot walk past it — whether the place exists yet or not.

## Packages

| Package | What it is |
|---------|------------|
| `pathguard` | The general judgement: resolving a path one link at a time, comparing it with places by identity and by name, the one list of places, and two file policies (Local and Outbound). Knows nothing of MCP. |
| `pathguard/workdir` | Organization ADR-021, the work-directory contract of the file-mediated MCP servers, built on `pathguard`: `work_dir` from the tool argument or `_meta`, a closed list of checks, the error codes. |

## Install

```bash
go get github.com/nlink-jp/pathguard
```

## Usage

### A server validating the caller's work directory

```go
import (
    "github.com/nlink-jp/pathguard"
    "github.com/nlink-jp/pathguard/workdir"
)

r := workdir.NewResolver(workdir.Options{
    Protected:    []pathguard.Place{pathguard.ServerDir(configDir, "")},
    RequiredHint: "Results come back as paths, and a path you cannot open is worth nothing.",
})

dir, err := r.Resolve(args.WorkDir, requestMeta) // argument, else _meta["jp.nlink/work_dir"]
var e *workdir.Error
if errors.As(err, &e) {
    // e.Code is work_dir_required / _invalid / _not_found / _not_writable / _denied
    // e.Details on work_dir_denied: {"work_dir", "resolved", "reason"}
}
```

A zero `workdir.Resolver` refuses everything — only `NewResolver` builds a
working one. If the home directory cannot be determined, or a protected place
has no absolute path (`ServerDir("")`), every call is refused and says why.

With `Options.Home` empty, the floor is built for the home directory the
environment names (`$HOME`) and, when it differs, for the account's own home
from the user database as well: a server started with `HOME` pointing
elsewhere still protects the real `~/.ssh`.

### A file the call names

```go
// Reading or writing it on this machine:
if reason, why := r.LocalPath(raw, resolved); why != "" { /* refuse with reason */ }

// Sending it off the machine (an upload):
if reason, why := r.OutboundPath(raw, resolved); why != "" { /* refuse */ }
```

A call site that holds no `Resolver` uses the package functions, which build
the policy from this process's home directories, as `Options.Home` empty does
(an unknown home refuses):

```go
if why := workdir.Sensitive(raw, resolved); why != "" { /* refuse */ }         // Local
if why := workdir.SensitiveOutbound(raw, resolved); why != "" { /* refuse */ } // Outbound
```

## The two file policies

| | Local — read or write here | Outbound — send off the machine |
|---|---|---|
| The real credential and agent-control places under **your** home (`~/.ssh`, `~/.aws`, `~/.kube`, `~/.config/gh`, `~/.netrc`, `~/.bash_history`, … ) | refused | refused |
| `.env` / `.env.*` anywhere (not `.env.example`, `.sample`, `.template`, `.dist`) | refused | refused |
| The same names elsewhere — `evidence/home/bob/.bash_history`, a project's `.npmrc` | allowed | refused |
| Secret names anywhere — `id_rsa`, `credentials.json`, `*service-account*.json` | allowed | refused |
| The server's protected directories | refused | refused |

A copy of a credential file in an incident-response collection is what an
analyst needs to read; a key that has left the machine cannot be taken back.
System locations refuse a work directory, not a file.

## How a path is compared

- **Every hop is a form.** Links are resolved one at a time; the path as given,
  every path on the way, and the final path are all checked. A link planted as
  `work/x → ~/.ssh/config`, where `~/.ssh/config` itself links into a sync
  folder, is refused because its middle form lies in `~/.ssh`.
- **Identity and name, always both.**
  - Identity anchors each place at the deepest part of its path that exists:
    the place itself, or the directory it would be created in, plus the names
    of the rest. A form matches when one of its own existing ancestors is the
    same file (`os.SameFile`) and its remaining names begin with the place's.
    That catches every spelling of what exists — case, links, firmlinks,
    `/.nofollow`, `/.vol`, Unicode normalisation, a hard link to a file such as
    `~/.netrc` — and it does so for `~/.aws/credentials` before `~/.aws` exists.
  - The name comparison still protects on a filesystem whose inode numbers
    cannot be trusted.
- **Names are folded the way the disk folds them.** APFS matches names by
  Unicode case folding, not ASCII lowercase: `id_rſa` opens `id_rsa`, the Kelvin
  sign opens `k`, `.ﬆ` opens `.st`. The name comparison folds the same way,
  including the expansions `ß` → `ss` and the Latin ligatures. Every character
  APFS equates with a protected name's letters is folded; normalisation
  (composed and decomposed `é`) is not, and identity covers it.
- **Where a credential directory's links lead is protected too.** If
  `~/.ssh/config` links into a sync folder, the file it points at is refused
  under its own name, and so is creating it there if it is missing. A link to the
  directory itself or above it (to `/`, to the home directory) protects only its
  own location; otherwise everything would be refused. The links inside a
  server's own directory are not followed; they may lead to work directories.
- **Exact places match only themselves.** `/`, `/private/var` and the home
  directory refuse a work directory that *is* them, not everything below them.
- **A path longer than any system opens is refused** (`unresolvable_path`): over
  4096 bytes, or 32 KiB on Windows. That bounds what one path argument can cost:
  about 2 ms for a realistic path, under 20 ms at the limit.

## Limits

This is a floor, not a boundary.

- **A verdict is a snapshot.** A link created between the check and the open is
  not seen. Open the resolved path the check was given, and confine writes with
  `os.Root` or `O_NOFOLLOW`; a server's own containment is where that race is
  closed.
- A hard link to a file inside a credential directory under another name, or a
  copy of a secret, is not detected by the Local policy.
- Only the links directly inside a credential or agent-control directory are
  followed to their targets. A link deeper inside (`~/.ssh/keys/work → …`)
  protects its own location, not where it leads. A link to a large directory
  (`~/.aws/x → ~/Dropbox`) protects all of it; the refusal names the link.
- Only the home directories of the account the server runs as are protected as
  places. Another user's `.claude`, `.gemini` and `.codex` are ordinary
  directories to both policies (gem-agent and lagent refuse them by name in any
  home). The Outbound policy still refuses another user's `.ssh`, `.aws` and the
  rest by name.
- On a filesystem with unstable inode numbers, identity can collide and refuse a
  legitimate path.
- The name of a place that does not exist yet is compared without Unicode
  normalisation below its deepest existing directory. That matters only for a
  protected directory with non-ASCII names that has not been created yet; the
  floor's names are ASCII.
- **Windows is reasoned, not measured.** The handling follows the platform's
  documented behaviour, compiles, and its name rules are unit-tested, but it has
  not been run on Windows:
  - every path goes through `filepath.Abs`, which is Windows' own normalisation;
  - names are compared without their stream suffix (`.env::$DATA`) and
    trailing dots and spaces;
  - junctions are followed as links;
  - a rooted link target (`\Users\u`) is on the link's drive.

  8.3 short names (`CREDEN~1.JSO`) reach an existing place by identity, but get
  past the Outbound policy's name-only rules.
- An empty or relative home is treated as unknown and refuses everything.

## Documentation

- [RFP](docs/en/pathguard-rfp.md) — the problem, the decisions, the plan
- Organization ADR-021 and ADR-022 in [nlink-jp/.github](https://github.com/nlink-jp/.github/tree/main/adr)

## License

MIT
