# pathguard

One judgement of whether a path may be touched, for [nlink-jp](https://github.com/nlink-jp) tools.

A Go library, standard library only. It decides whether a path lies in a place
nothing may reach — system locations, credential stores, an agent's
configuration, a server's own directories — and does so by **file identity as
well as by name**, so a case variant on a case-insensitive disk, a symlink, or a
firmlink cannot walk past it.

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
working one. If the home directory cannot be determined, every call is refused
and says why.

### A file the call names

```go
// Reading or writing it on this machine:
if reason, why := r.LocalPath(raw, resolved); why != "" { /* refuse with reason */ }

// Sending it off the machine (an upload):
if reason, why := r.OutboundPath(raw, resolved); why != "" { /* refuse */ }
```

A call site that holds no `Resolver` uses the package functions, which build
the policy from this process's home directory (an unknown home refuses):

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
- **Identity and name, always both.** Identity (`os.SameFile` against the form
  and each existing directory above it) catches every spelling of a place that
  exists — case, links, firmlinks, a hard link to a file such as `~/.netrc`. The
  name comparison covers a place that does not exist yet and a filesystem whose
  inode numbers cannot be trusted.
- **Names are folded the way the disk folds them.** APFS matches names by
  Unicode case folding, not ASCII lowercase: `id_rſa` opens `id_rsa`, the Kelvin
  sign opens `k`, `.ﬆ` opens `.st`. The name comparison folds the same way,
  including the expansions `ß` → `ss` and the Latin ligatures.
- **Where a place's links lead is protected too.** If `~/.ssh/config` is a link
  into a sync folder, the file it points at is refused under its own name.
- **Exact places match only themselves.** `/`, `/private/var` and the home
  directory refuse a work directory that *is* them, not everything below them.

## Limits

This is a floor, not a boundary.

- A hard link to a file inside a credential directory under another name, or a
  copy of a secret, is not detected by the Local policy.
- Only the links directly inside a place are followed to their targets; a link
  deeper inside (`~/.ssh/keys/work → …`) protects its own location, not where it
  leads.
- Only the home directory of the account the server runs as is protected as a
  place. Another user's `.claude`, `.gemini` and `.codex` are ordinary
  directories to both policies (gem-agent and lagent refuse them by name in any
  home); the Outbound policy still refuses another user's `.ssh`, `.aws` and the
  rest by name.
- On a filesystem with unstable inode numbers, identity can collide and refuse a
  legitimate path. Unicode normalisation (`é` composed or decomposed) is
  matched by identity only, so not for a place that does not exist yet.
- An empty or relative home is treated as unknown and refuses everything.

## Documentation

- [RFP](docs/en/pathguard-rfp.md) — the problem, the decisions, the plan
- Organization ADR-021 and ADR-022 in [nlink-jp/.github](https://github.com/nlink-jp/.github/tree/main/adr)

## License

MIT
