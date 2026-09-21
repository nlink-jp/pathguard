# CLAUDE.md — pathguard

Organization rules: https://github.com/nlink-jp/.github/blob/main/CONVENTIONS.md
Read AGENTS.md first.

## Non-negotiable rules

- **Standard library only.** `go.mod` gains no `require` line; two consumers
  depend on it.
- **Fail closed.** An unknown home, a zero value, an unresolvable chain of links:
  refuse, never pass.
- **Both comparisons, every time.** Do not replace the name comparison with
  identity or the other way round.
- **The list changes with the runtimes'.** Edit `floor.go`,
  `testdata/runtime-lists.json` and gem-agent/lagent's `lane.go` together.
- **Tests with every change**, and a mutation check for any new rule: a rule no
  test fails without is not a rule.
- **Docs in sync**: README.md and README.ja.md in the same commit.
