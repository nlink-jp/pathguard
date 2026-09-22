# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project adheres
to [Semantic Versioning](https://semver.org/).

## [0.3.0] - 2026-09-22

### Added

- `Where(p) (end string, ok bool)`: where a path ends — every link followed, a
  dangling one by its target; for an existing path what `filepath.EvalSymlinks`
  returns. The place to judge a path at before asking whether a file is there.
  The last of `Forms` is not always that end: the forms are de-duplicated, so a
  chain of links that comes back to an earlier spelling ends on an earlier form.

### Documentation

- Known limits recorded in README and the RFP: a `..` climbing out through a
  credential directory's entry is judged where it lands; `work_dir` validation
  keeps ADR-022 §4's order; hard links into credential directories and to
  `.env`; Unicode normalisation of link targets; about 2 ms per check. README
  also notes that tests which redirect `HOME` still list the account's real
  credential directories unless built with `-tags osusergo`.

## [0.2.0] - 2026-09-22

### Added

- `workdir.Resolver.CheckBeneath(dir)`: the places that refuse a work directory,
  applied to a directory beneath it that a server actually uses — a workspace
  `<work_dir>/<workspace_id>`, existing or not. Validating `work_dir` alone let
  `work_dir=~/.config` with `workspace_id=gh` land in `~/.config/gh`.

### Fixed

- A path holding a NUL byte is refused as `unresolvable_path`. A path handed to
  C ends at the first NUL, so `.netrc\x00.safetensors` was judged as one string
  and opened as another.

## [0.1.0] - 2026-09-22

### Added

- `pathguard`: whether a path lies in a place, resolved one link at a time (every
  hop is a form) and compared by file identity and by case-folded name; the one
  list of places (`Floor`) — system locations, the credential directories and
  files of gem-agent's and lagent's list, the agent-control directories; the
  name rules (`EnvFile`, `SecretName`, `CredentialSegment`); and two file
  policies, `Local` (read or write on this machine) and `Outbound` (send off
  it).
- `pathguard/workdir`: organization ADR-021 on top — `NewResolver`, `Resolve`
  (argument, else `_meta["jp.nlink/work_dir"]`), `Validate` (the closed list of
  checks), `LocalPath`, `OutboundPath`, and the fleet's error codes.
- `workdir.Sensitive` / `workdir.SensitiveOutbound` for call sites that hold no
  `Resolver`; `work_dir_denied` carries `details.reason`.
- Identity is anchored: a place that does not exist yet is found through any
  spelling of the directory it would be created in (a firmlink, `/.nofollow`,
  `/.vol`, a linked parent).
- Names are compared by Unicode case folding as APFS applies it (`id_rſa` is
  `id_rsa`), not by ASCII lowercase.
- The targets of the links directly inside a credential or agent-control
  directory are protected, dangling ones included, except a link to the
  directory itself or above it.
- A `..` after a missing component is resolved against what exists.
- Fail closed: an empty or relative home, a working directory that cannot be
  read, and a protected place without an absolute path (`ErrBadPlace`) all
  refuse everything.
- With `Options.Home` empty, the account's own home is protected as well when
  `$HOME` names another.
- A path longer than any system opens (4096 bytes; 32 KiB on Windows) is
  refused, and so is any longer form link hops produce. One check costs about
  2 ms, and about 0.33 s at worst (39 planted links, every form at the cap).
- Windows handling (not run on Windows): `filepath.Abs` normalisation, stream
  suffixes and trailing dots in names, junctions, rooted link targets.
- Built to replace the nine per-server copies of `internal/workdir`, which
  compared locations by name on a case-insensitive disk; the servers move to
  it one release at a time.
