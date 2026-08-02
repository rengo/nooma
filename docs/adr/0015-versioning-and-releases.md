# ADR-0015 — Versioning: SemVer, tags from M1, and no `beta` label

- **Status**: Accepted
- **Date**: 2026-08-02
- **Supersedes**: —
- **Superseded by**: —
- **Enables**: M1 (first tag), M6 (release automation)

## Context

`cmd/nooma/main.go` declares `var version = "dev"` with a comment promising it is "overridden at
build time with `-ldflags "-X main.version=..."`". Nothing overrides it. No Makefile target passes
`-ldflags`, so every binary ever built from this tree — including the M0 binary the build plan calls
"runnable and demonstrable" — reports `nooma dev (<sha>)`. The hook exists; the rope was never tied.

The repository has **zero tags**, and `docs/05-build-plan.md` defers all release machinery to M6
("Goreleaser / multi-platform binary CI"). That deferral is defensible for *publishing*, but it has
silently deferred *numbering* too, and those are different problems. M0 is closed and M1 is
in flight with no way to name what M0 produced.

Three constraints shape the decision:

- **[ADR-0005](0005-v1-scope.md) already defines what `1.0.0` means.** v1 is the complete cognitive
  loop; perception moves to v2. The scope question is settled, so this ADR only has to bind a number
  to it — not relitigate it.
- **There are two independent version numbers in this system.** The binary's, and the vault's
  `PRAGMA user_version` (an integer, currently `2`, one per published migration). They advance for
  unrelated reasons: a release that only changes the UI touches no migration.
- **The vault format is not stable and will not be before v1.** M2 adds consolidation state, M5 adds
  the learner's tables. Promising compatibility now would be a lie the schema is going to break.

## Options evaluated

| Option | Pro | Con |
|---|---|---|
| **SemVer, `0.x` per milestone, `1.0.0` at M6** | Standard, machine-parseable, and `0.x` is the honest signal for a format still moving; costs one annotated tag per milestone | Requires deciding the M→MINOR mapping up front |
| Start at `1.0.0` now | Looks finished | `1.0.0` is a compatibility promise. Breaking the vault at M2 would force `2.0.0` by M2 and `5.0.0` by M5 — MAJOR bumps that describe nothing |
| CalVer (`2026.08.0`) | No judgment call per release; encodes recency | Encodes *when*, never *whether it breaks you*. For a self-hosted binary migrating a user's own vault, "will this eat my data" is the only question a version must answer |
| No versions until M6 | Zero ceremony now | Already the status quo, and it is why M0 shipped unnameable. A tag is free; the cost is paid later, in archaeology |
| Couple the binary version to `user_version` | One number to reason about | Forces an empty migration per release, or freezes releases to schema changes. Two facts, two numbers |

Separately, on how to label pre-1.0 instability:

| Option | Verdict |
|---|---|
| **`0.x` alone carries the "unstable" signal** | Chosen. SemVer §4 already assigns `0.y.z` exactly this meaning |
| `0.5.0-beta.1` | Rejected — redundant. Every `0.x` is a beta; the suffix adds sort complexity and says nothing new |
| `1.0.0-rc.N` before `1.0.0` | Kept. Here the suffix earns its place: it marks a candidate for a version that *does* promise compatibility |

## Decision

**Versions follow [SemVer 2.0.0](https://semver.org). Tagging starts at M1, not M6.**

1. **Format**: `MAJOR.MINOR.PATCH`, tagged with a `v` prefix — `v0.1.0`. Never `0.01`: SemVer has
   three integer components and no decimal fraction, and a two-component string is unparseable by
   `git describe`, Go modules, and Goreleaser alike.

2. **`0.x` is the pre-v1 contract, stated plainly**: the CLI surface and the vault format may break
   between MINOR versions. No `-beta` suffix is used to say this, because `0.y.z` already says it.

3. **One MINOR per milestone**, tagged when its demo criterion in `docs/05-build-plan.md` passes:

   | Milestone | Tag |
   |---|---|
   | M1 capture and recall | `v0.1.0` |
   | M2 sleep and weight | `v0.2.0` |
   | M3 Telegram and prospection | `v0.3.0` |
   | M4 complete UI | `v0.4.0` |
   | M5 the learner | `v0.5.0` |
   | M6 release polish | `v1.0.0-rc.N`, then `v1.0.0` |

   PATCH is for fixes landing between milestones. M0 is not tagged retroactively: a tag records a
   release that happened, and no one could have installed M0.

4. **`v1.0.0` is cut when M6's demo criterion passes** — "installable release used by a stranger
   without help" — and not before. It is a promise, not a celebration: from that tag on, breaking the
   CLI or requiring a non-automatic vault migration means `2.0.0`.

5. **The binary version and `PRAGMA user_version` are never coupled.** They answer different
   questions and move on different schedules. `nooma status` reports the schema version; `nooma
   version` reports the binary's.

6. **The annotated Git tag is the source of truth; the GitHub Release is presentation.** The tag is
   the immutable pointer that survives leaving GitHub. The Release hangs off it with notes and
   attached binaries, and from M6 is produced by Goreleaser on tag push. Tag first, always.

7. **The version is derived from the tag, not written in a file.** `git describe --tags --dirty`
   feeds `-ldflags -X main.version=`, so an untagged working tree keeps reporting `dev` and a tagged
   one cannot disagree with its tag. There is no `VERSION` file and no constant to forget to bump.

## Consequences

### What it enables

- `nooma version` starts telling the truth — the promise in `main.go`'s comment becomes real.
- A user can say "it broke in `v0.3.0`" and a maintainer can check out exactly that tree.
- `v0.1.0` through `v0.5.0` set the expectation that vaults may need rebuilding, so the first
  breaking migration is a documented consequence rather than an incident.
- Goreleaser at M6 needs no new decision: it consumes tags that already exist in the format it wants.

### What it costs

- One annotated tag per milestone, and the discipline to cut it at the demo criterion rather than
  when it feels done.
- Between tags, `git describe` reports `v0.1.0-7-gabc1234`, which is correct but not pretty. That is
  a development build, and it should not look like a release.
- `v1.0.0` becomes a commitment with teeth. That is the point, and it is also the cost.

### Reversal criteria

Write the ADR that supersedes this one if:

- v1 ships and the vault format keeps breaking anyway, so MAJOR climbs for reasons that do not
  describe user-visible change — the mapping would then be wrong, not the standard.
- Distribution moves somewhere with an incompatible version grammar (a package registry that rejects
  pre-release suffixes, say), forcing the tag format rather than the policy to change.
- Milestones stop being the unit of release — if M4 ships in four independently useful slices,
  "one MINOR per milestone" is describing a plan that no longer exists.
