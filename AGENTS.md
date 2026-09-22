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
├── place.go        # Place, Kind, ServerDir, Check: anchored identity + folded name; link targets
├── fold.go         # fold/segKey/foldPath: Unicode case folding as APFS applies it; Windows names
├── floor.go        # the one list (Floor), ErrNoHome
├── names.go        # EnvFile, SecretName, CredentialSegment (name rules)
├── policy.go       # Local and Outbound file policies
├── pathguard_test.go
├── anchor_test.go  # anchors, link targets, the name layer alone, Windows names, BenchmarkLocalCheck
├── testdata/runtime-lists.json   # copy of gem-agent/lagent's list + outbound verdicts
├── workdir/        # ADR-021: Resolver, NewResolver, Resolve/Validate, Local/OutboundPath,
│                   #   Sensitive/SensitiveOutbound (no Resolver at hand)
└── docs/{en,ja}/   # RFP
```

## Gotchas

- **Compare places by identity and by name, always both.** Identity is
  *anchored*: a place is its deepest existing ancestor-or-self (`look`,
  `anchors[0]`) plus the folded names below it, and a form matches when one of
  its existing ancestors is the same file and its remaining names begin with
  the place's. That is what catches a place that does not exist yet under any
  spelling of its parent (firmlink, `/.nofollow`, `/.vol`, a linked parent);
  comparing a missing place by its spelling alone let all of those through.
  The name comparison covers filesystems with unstable inode numbers. Each is
  tested on its own — `noIdentity` switches identity off — and a mutation that
  drops either, for tree, exact or link-target places, fails a test; keep it so.
- **Test seams are package variables:** `statFn` (identity), `getwd`,
  `windowsNames` (Windows name rules on any platform), `workdir.accountHome`.
  Restore them in `t.Cleanup`.
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
- **Link targets are protected one level deep, for Credential and AgentControl
  places only.** `linkTargets` reads the place's own entries and adds each
  link's target, dangling or not, as a place. It skips a target that is the
  place or above it (`reachesUp`), and the links in a server's directory,
  which may lead to work directories. Deeper links are a documented limit.
- **A place without an absolute path is `ErrBadPlace`,** and refuses every
  call; an empty `Reason`/`Why` gets default words. An empty `why` means
  "allowed" to every caller, so a place must never produce one.
- **One check costs about 2 ms** (`BenchmarkLocalCheck`, Apple Silicon), also
  for the longest path the cap lets through (`BenchmarkLocalCheckLongestPath`),
  and about 0.33 s for the worst case (`BenchmarkLocalCheckLinkChainAtTheCap`:
  39 planted links, every form at the cap, each ancestor stat resolving the
  chain in the kernel). The cap applies to every form in `forms`, hop forms
  included — a 97-byte path through one link with a long relative target once
  grew to 40 KB forms and cost 21 s. `viewsOf` looks at nothing once any path
  is unresolvable. Do not "optimise" `look` by stopping at the first missing
  ancestor: existence is not monotone (`/.vol/<dev>` does not stat, while
  `/.vol/<dev>/<ino>` does — measured), and the `/.vol` anchor would be lost.
  `ancestors`, `parent` and `child` are the linear substitutes for
  `filepath.Dir`/`Join`; `TestTheLinearWalksAgreeWithFilepath` pins them to the
  same strings. Linearity itself is shown by the benchmarks, not a test. Every place's forms and ancestors are
  looked up per call, and nothing is cached, on purpose. The agent controls the
  path, so keep `look` linear: an anchor's `rest` is a shared slice, never
  copied. A prepend per segment once made a 120 KB path cost 14 s, and
  `TestLookingAtALongPathAllocatesLinearly` guards it. `maxPathBytes` is
  `PATH_MAX` (4096) on Unix and 32 KiB on Windows.
- **Windows code is reasoned, not measured** (no Windows machine): `absolute`
  runs every path through `filepath.Abs`, `joinTarget` puts a rooted target on
  the link's drive, `linkMode` follows junctions, `windowsName` drops stream
  suffixes and trailing dots/spaces. Say so wherever it is described.
- **An unknown or relative home refuses; a zero `Policy` / `Resolver` refuses;
  `workdir.Sensitive` refuses when no home is known.** With `Options.Home`
  empty, the account's home from `os/user` is protected too when `$HOME` names
  another. Failing open here is how a floor silently disappears.
- **The credential list is the runtimes' list.** `testdata/runtime-lists.json`
  holds gem-agent's and lagent's `internal/sandbox/lane.go` lists; the tests hold
  this module to it and `check-org.sh` holds both runtimes to it. Change all
  three together.
- **`go 1.23`, no `require`.** The oldest consumer is on 1.23; two consumers
  promise no third-party dependency, and `TestTheModuleRequiresNothing` keeps
  the promise for them.
- **Messages are the fleet's wording.** Servers' agents learned them; change them
  deliberately.
