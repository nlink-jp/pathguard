# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project adheres
to [Semantic Versioning](https://semver.org/).

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
  refused, and one check costs time linear in the path's length.
- Windows handling (not run on Windows): `filepath.Abs` normalisation, stream
  suffixes and trailing dots in names, junctions, rooted link targets.
- Replaces the nine per-server copies of `internal/workdir`, which compared
  locations by name on a case-insensitive disk.
