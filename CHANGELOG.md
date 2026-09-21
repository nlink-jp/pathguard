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
- Names are compared by Unicode case folding as APFS applies it (`id_rſa` is
  `id_rsa`), not by ASCII lowercase; the targets of the links directly inside a
  protected directory are protected too; a `..` after a missing component is
  resolved against what exists; an empty or relative home refuses everything.
- Replaces the nine per-server copies of `internal/workdir`, which compared
  locations by name on a case-insensitive disk.
