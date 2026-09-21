# AGENTS.md — pathguard

## Summary

Go library deciding whether a path may be touched: system locations, the
credential floor, agent-control directories and a server's own directories,
compared by file identity and by name. Two layers: `pathguard` (general) and
`pathguard/workdir` (organization ADR-021, the MCP servers' work-directory
contract). Standard library only.

## Build & Test

```bash
go test ./...          # all tests
golangci-lint run ./...
```

No Makefile — this is a library, not a binary.

## Structure

```
pathguard/
├── forms.go        # Forms: resolve a path one link at a time; every hop is a form
├── place.go        # Place, Kind, ServerDir, Check (identity + folded name), link targets
├── fold.go         # fold/foldPath: Unicode case folding as APFS applies it
├── floor.go        # the one list (Floor), ErrNoHome
├── names.go        # EnvFile, SecretName, CredentialSegment (name rules)
├── policy.go       # Local and Outbound file policies
├── pathguard_test.go
├── testdata/runtime-lists.json   # copy of gem-agent/lagent's list + outbound verdicts
├── workdir/        # ADR-021: Resolver, NewResolver, Resolve/Validate, Local/OutboundPath,
│                   #   Sensitive/SensitiveOutbound (no Resolver at hand)
└── docs/{en,ja}/   # RFP
```

## Gotchas

- **Compare places by identity and by name, always both.** Identity catches
  case variants, links, darwin firmlinks and hard links to file entries; the
  name comparison covers places that do not exist yet and filesystems with
  unstable inode numbers. Tests exist for each; a mutation that drops either
  fails them — keep it so.
- **Forms are whole paths, never a link's location alone.** `/var` is met on the
  way to every darwin temporary directory; as a form it would match the exact
  place `/private/var` and refuse every temp dir.
- **`.` and `..` are applied to the part already walked**, which holds no link
  (`step`): `p2/../q` follows `p2` before climbing. Never `filepath.Clean` a
  path before resolving it.
- **A ".." after a missing component is walked again** (`again` in `step`): it
  climbs back into what exists, where a link may sit.
- **Exact places match the form itself, never an ancestor.**
- **Never compare names with `strings.ToLower` or `strings.EqualFold`.** APFS
  folds by Unicode: `ſ` is `s`, the Kelvin sign is `k`, `ﬆ` is `st`, `ß` is
  `ss` (measured 2026-09-22). Use `fold` / `foldPath`;
  `TestFoldAgreesWithTheUnicodeFoldsTheDiskApplies` checks the pairs against
  the disk it runs on.
- **Link targets are protected one level deep.** `linkTargets` reads a
  non-system directory place's own entries and adds each link's target as a
  place; deeper links are a documented limit, not an oversight.
- **An unknown or relative home refuses; a zero `Policy` / `Resolver` refuses;
  `workdir.Sensitive` refuses when `os.UserHomeDir` fails.** Failing open here is
  how a floor silently disappears.
- **The credential list is the runtimes' list.** `testdata/runtime-lists.json`
  holds gem-agent's and lagent's `internal/sandbox/lane.go` lists; the tests hold
  this module to it and `check-org.sh` holds both runtimes to it. Change all
  three together.
- **`go 1.23`, no `require`.** The oldest consumer is on 1.23; two consumers
  promise no third-party dependency, and `TestTheModuleRequiresNothing` keeps
  the promise for them.
- **Messages are the fleet's wording.** Servers' agents learned them; change them
  deliberately.
