# Spec — M0: the binary becomes runnable

Delta specification for the `m0-skeleton` change. This document states what MUST be true of the
repository after the change is applied, in testable form. It does not prescribe how (that is
`design.md`). Sources: `proposal.md`, `docs/01-architecture.md`, `docs/03-data-model.md`,
`docs/05-build-plan.md`, `docs/06-harness.md`, `docs/adr/0007-http-auth.md`, `CLAUDE.md`.

Scope boundary (binding, from the proposal §3.1): **M0 wires what exists and adds only what a
running binary needs. It creates no domain concept.** Every requirement below is bounded by that
line.

---

## 1. Documentation corrections

These land before any loader code, in a docs-only PR. They are prerequisites: the loader cannot
be written against a document that contradicts itself.

### R1.1 — `nooma.yml` and `.env` live inside the vault

**MUST**: `docs/01-architecture.md` shows configuration and secrets at the vault root in every
place it depicts them. The installed-mode tree MUST NOT show a `nooma.yml` beside the vault
directory.

**Verified by**: reading the document; the tree at §"The binary and the vault" and the §"Vault
structure" listing agree, and §"Configuration — `nooma.yml`" ("a YAML file at the root of the
vault") remains true of both.

**Scenario**:
- GIVEN a reader who only looks at the ASCII trees
- WHEN they compare them against the prose in §"Configuration"
- THEN they find the same location for `nooma.yml`, with no third option

### R1.2 — Vault resolution is stated in full, including the upward search and the zero/multiple cases

**MUST**: `docs/01-architecture.md` §"Vault resolution at startup" states four ordered steps and,
for the globbing steps, what happens when the search finds zero usable candidates and when it
finds more than one.

**MUST**: step 3 is described as an upward search — starting at the current working directory and
walking up one directory at a time to the filesystem root, testing whether each directory visited
is itself a vault or contains exactly one usable `*.nooma` directory — not a single check of the
cwd and its immediate children, and no longer "vault next to the executable". See R6.1, R6.2 and
R6.6 for why and how.

**MUST**: the document states there is no `$HOME` ceiling on the ascent — it runs to the
filesystem root, matching the git precedent it is modeled on.

**MUST**: step 4 names a `*.nooma` directory inside `~/.nooma/`, not `~/.nooma/` itself, and the
document notes that the literal name `.nooma` is never itself treated as a step-3 candidate
(R6.7).

**MUST**: the document states that an explicit vault argument may be relative (R6.4).

**Verified by**: reading the document; the four steps, the upward-search description, the
no-ceiling statement, the `.nooma`-name exclusion, and the three candidate-count outcomes are all
present, and the phrase "next to the executable" no longer appears.

### R1.3 — The `server:` block documents `bind` and `auth_token_env`

**MUST**: the `nooma.yml` example in `docs/01-architecture.md` includes `server.bind` (with
`127.0.0.1` as the documented default) and `server.auth_token_env`, both required by
[ADR-0007](../../../docs/adr/0007-http-auth.md).

**MUST NOT**: `docs/adr/0007-http-auth.md` be edited. It is `Accepted`; the doc that was
incomplete is doc 01 (non-negotiable #2).

**Verified by**: R9.1's config↔doc gate, which fails if a documented key has no struct field or a
struct field has no documented key. This requirement is the reason that gate cannot pass on the
document as it stands today.

### R1.4 — No other doc claim about M0 is left contradicted

**MUST**: `docs/01-architecture.md`'s CLI table entry for `nooma status` agrees with what M0
actually reports, or states explicitly which parts arrive in a later milestone.

**Rationale**: doc 01 lists "last consolidation, channels, size" for `status`. Last consolidation
is a domain row, excluded by the scope boundary. Non-negotiable #1 requires that a divergence
between code and doc be fixed in the same PR — including when the fix is to date the claim.

---

## 2. CI triggers

### R2.1 — L4 and the cross-compile matrix run on every pull request, across seven targets

**MUST**: the e2e job and the cross-compilation matrix job run on `pull_request`, not only on
`push` to `main`.

**MUST**: the cross-compilation matrix builds seven targets — the cartesian six, `linux`,
`darwin`, `windows` × `amd64`, `arm64`, plus `linux/arm` — per the new ADR (ADR-0013, superseding
ADR-0001's acceptance criterion 5; `docs/adr/0001-sqlite-driver.md` is `Accepted` and is not
edited). See design.md D13 for why `linux/arm` is retained rather than the cartesian six alone.

**Verified by**: `.github/workflows/*.yml` declare a `pull_request` trigger for both jobs and a
seven-entry matrix for cross-compile; a PR in this chain shows both as checks.

**Scenario**:
- GIVEN a PR that breaks the build for `windows/arm64` only
- WHEN CI runs on that PR
- THEN the cross-compile job fails on that PR, before the merge, not after

### R2.2 — Every matrix leg blocks the merge, not just "the two jobs"

**MUST**: the repository ruleset's `required_status_checks` includes **every context each
matrix-generated leg posts**, not one context per job. `main.yml`'s cross-compile job templates
its check name per matrix leg (`name: cross-compile ${{ matrix.goos }}/${{ matrix.goarch }}`), so
it posts one context per leg — **7**, matching R2.1's seven-target matrix — and the e2e job's
`windows-latest` addition (D6) means it posts one context per OS leg — **2**. After this change
that is **9** distinct required contexts from these two jobs, not 2.

**MUST NOT**: the ruleset register only two job-level names (e.g. `cross-compile` and `e2e`) when
the workflows post per-leg names instead — a required context that never posts is never satisfied,
which does not degrade the gate, it **permanently blocks every future merge to `main`** with no
recovery short of editing the ruleset itself. Getting this count wrong is not a soft failure.

**MUST**: alternatively, a matrix-aware mechanism may be used instead of enumerating per-leg
context strings — for example, a single synthetic "all matrix legs succeeded" job that the other
legs report into, registered as the one required context — provided it is verified to actually
block on every leg's failure, not only the first to report.

**Verified by**: reading the applied ruleset; either the 9 leg-specific context strings are present
and each matches a `name:` value the workflow actually produces for that leg, or a matrix-aware
synthetic gate is present and its dependency on every leg is verified by inspecting the workflow.

**Note**: this is the only requirement in this spec that cannot be verified by a local command —
it lives in GitHub configuration, like the existing seven contexts (unrelated to this change,
predating it).

### R2.3 — `make cross-compile` joins `check-all`

**MUST**: the Makefile gains a `cross-compile` target that builds the same seven `GOOS`/`GOARCH`
pairs as R2.1's CI matrix, and `make check-all` includes it.

**MUST NOT**: `check-all`'s own documentation (`CLAUDE.md`'s Workflow section, the `Makefile`
header) claim a parity with CI it does not have.

**Verified by**: `make cross-compile` exits zero locally; `check-all`'s target list in the
Makefile includes `cross-compile`.

**Rationale**: `CLAUDE.md`'s Workflow section: "if you add a blocking CI job, add it to
`check-all` too — unless it needs PR metadata a Makefile cannot produce." Cross-compilation needs
none — it is `GOOS=x GOARCH=y go build ./...` — and neither does `make test-e2e` (see R2.4), unlike
`docs-sync.yml` (which needs PR labels).

### R2.4 — `make test-e2e` joins `check-all` too

**MUST**: `make check-all` includes `test-e2e` once R2.1 makes the e2e job block pull requests.

**MUST**: `CLAUDE.md`'s Workflow section and the `Makefile` header are updated in the same PR to
name both new targets, so their enumeration of what `check-all` covers stays true.

**MUST NOT**: any document claim that e2e is excluded from `check-all` "for a documented reason".
No such reason exists anywhere in the repository — `CLAUDE.md` names exactly one gate `check-all`
cannot cover, `docs-sync.yml`, for exactly one reason, PR metadata. An earlier draft of this spec
and of design §7 both asserted the nonexistent exclusion, and design §7 then contradicted itself
eleven lines later by saying e2e was added.

**Verified by**: `check-all`'s target list in the Makefile includes `test-e2e`; `make check-all`
exits zero on a clean checkout.

**Rationale**: `make test-e2e` already exists, needs no PR metadata, and runs locally, so
`CLAUDE.md`'s rule applies to it literally the moment R2.1 makes it a blocking gate. It is absent
from `check-all` today only because it did not block anything today. The cost is accepted and
stated: `check-all` now compiles the binary and runs L4, so it gets slower. That is what
`check-all` is for; `make check` stays the fast loop and is untouched.

---

## 3. Configuration schema and decoding

### R3.1 — The schema covers every key documented in doc 01

**MUST**: `internal/config` defines Go types covering `server`, `database`, `providers`, `tasks`,
`channels` and `schedules`, including every key shown in `docs/01-architecture.md`
§"Configuration — `nooma.yml`" as corrected by R1.3.

**MUST**: keys whose semantics belong to a later milestone (`providers`, `tasks`) are decoded and
shape-checked, and MUST NOT be interpreted in M0.

**Verified by**: R9.1's gate (the documented example decodes), plus L1 tests decoding a full
config and asserting every field.

### R3.2 — An unknown key is a load error

**MUST**: decoding a `nooma.yml` containing a key not present in the schema fails with an error
naming the offending key and its path.

**MUST NOT**: any code path ignore, skip, or silently drop an unrecognized key.

**Verified by**: an L1 table test with one case per nesting level — top level, inside `server`,
inside a named provider, inside a named task, inside `channels.telegram`, inside `schedules`.

**Scenario**:
- GIVEN a `nooma.yml` with `server: { http_prot: 7777 }` (a typo for `http_port`)
- WHEN the config loads
- THEN loading fails, the message names `http_prot` and where it appeared
- AND the process does not fall back to a default port

**Rationale**: `docs/01-architecture.md` sells the configuration on "explicit tasks, nothing
hidden in defaults". A loader that ignores an unknown key breaks that promise in its most
expensive form: the user corrects a mistake, restarts, and observes no change.

### R3.3 — A malformed document fails; it does not partially apply

**MUST**: a YAML syntax error, or a value of the wrong type, fails the load with a message
carrying the line or key path. No partially-populated config is returned to the caller.

**Verified by**: L1 tests asserting a nil/zero config and a non-nil error together.

### R3.4 — Defaults are explicit and few

**MUST**: exactly these keys may be absent, taking a documented default: `server.bind`
(`127.0.0.1`), `server.http_port` (`7777`), `server.ui` (`true`), `database.path`
(`./nooma.db`).

**MUST**: every other absent key either is legitimately optional (an empty `channels`, an empty
`providers`) or fails validation per §5.

**Verified by**: an L1 test loading a minimal config (an empty document, or one containing only
`{}`) and asserting each default's value.

### R3.5 — The parsed config is inspectable and printable

**MUST**: the loaded config can be rendered as text for `status` and `doctor` without exposing a
secret value (see R4.2).

**Verified by**: an L1 test that populates the environment with a sentinel secret, renders the
config, and asserts the sentinel does not appear in the output.

---

## 4. Secrets

### R4.1 — Secrets are referenced by environment variable name, never inlined

**MUST**: the schema accepts only `*_env` keys for credentials (`api_key_env`, `bot_token_env`,
`auth_token_env`). A key holding a literal credential (`api_key`, `bot_token`, `auth_token`) is
not in the schema and therefore fails R3.2.

**Verified by**: an L1 test asserting that a config with `api_key: sk-...` fails to load, naming
the key.

**Scenario**:
- GIVEN a user who pastes their API key directly into `nooma.yml`
- WHEN the config loads
- THEN it fails, naming `api_key`, rather than working and committing a secret to their vault

### R4.2 — A rendered config never contains a secret value

**MUST NOT**: any output of `status`, `doctor`, or an error message contain the *value* of an
environment variable referenced by a `*_env` key. Referencing the variable's *name* is required
and expected.

**Verified by**: R3.5's sentinel test, and an L4 test running `nooma status` and `nooma doctor`
with a sentinel value in the environment, asserting it appears in neither stdout nor stderr.

### R4.3 — `.env` is loaded from the vault root, and the real environment wins

**MUST**: after vault resolution and before config decoding, the loader reads `<vault>/.env` if
present and applies its assignments **without overwriting** a variable already set in the process
environment.

**MUST**: the same `KEY` appearing twice inside one `.env` file is a load error naming the file and
both line numbers.

**MUST**: a bare, unquoted `#` appearing after a value on the same line is rejected as ambiguous,
not absorbed into the value.

**Verified by**: L1 tests for three cases — variable only in `.env`, variable only in the
environment, variable in both (the environment's value wins) — plus L1 cases for a duplicate key
within one `.env` file and for `KEY=value # comment` (rejected, not read as
`"value # comment"`).

**Scenario**:
- GIVEN a `.env` containing `ANTHROPIC_API_KEY=from-file`
- AND a process environment already containing `ANTHROPIC_API_KEY=from-operator`
- WHEN the config loads
- THEN the effective value is `from-operator`

**Rationale**: an operator overriding a value for one run (a container, a systemd unit, a
one-off) must not be silently overridden by a file on disk. The duplicate-key and bare-`#` rules
apply the same "reject what it does not understand" stance §3 takes on YAML duplicate keys and §6
takes on vault discovery, to the third format this project parses by hand.

### R4.4 — A missing `.env` is not an error

**MUST**: the absence of `<vault>/.env` loads successfully. A vault with every secret supplied by
the environment is valid.

**Verified by**: an L1 test on a vault directory containing no `.env`.

---

## 5. Validation: structural safe defaults

Non-negotiable #7: *safe defaults are structural, not warnings*. These are enforced at load time,
before the components they protect exist.

### R5.1 — A Telegram channel enabled without `allowed_chat_ids` fails validation

**MUST**: `channels.telegram.enabled: true` with an absent or empty `allowed_chat_ids` fails
validation with a message stating why.

**MUST**: this holds in M0, even though M0 starts no channel.

**Verified by**: L1 tests for enabled-and-empty (fails), enabled-with-ids (passes),
disabled-and-empty (passes).

**Rationale**: deferring the check to M3, when the adapter arrives, leaves the promise as future
discipline instead of a gate. The requirement costs a validation branch now.

### R5.2 — A referenced environment variable that is unset fails validation for the components that need it

**MUST**: validation reports a `*_env` key whose named variable is unset **when the component
that consumes it is enabled**. A provider that no enabled component uses does not fail the load
in M0.

**Verified by**: L1 tests — `channels.telegram.enabled: true` with `bot_token_env: X` and `X`
unset fails; the same with `enabled: false` passes.

**Rationale**: M0 interprets no provider (R3.1), so failing on every unset provider key would
make a config that is correct for M1 unloadable in M0.

### R5.3 — `database.path` resolves inside the vault

**MUST**: a relative `database.path` resolves against the vault root.

**MUST**: a path that escapes the vault root after cleaning is a validation error.

**MUST**: the path handed to `internal/store/sqlite.Open` is absolute — that function rejects a
relative path (`ErrRelativeDBPath`).

**Verified by**: L1 tests for `./nooma.db` (resolves to `<vault>/nooma.db`), `sub/dir/nooma.db`
(allowed), `../outside.db` (error), and an absolute path outside the vault (error).

**Scenario**:
- GIVEN `database: { path: ../shared/nooma.db }`
- WHEN the config loads
- THEN validation fails, stating that the database must live inside the vault

**Rationale**: derived from the layout decision — the vault is one object that can be copied,
moved and backed up as a unit. A database outside it breaks that guarantee silently, and the
breakage only becomes visible when a backup is restored.

### R5.4 — Validation reports every problem it can, not just the first

**MUST**: validation of an otherwise-loadable config collects and reports all violations it can
detect independently, rather than returning after the first.

**Verified by**: an L1 test with a config violating R5.1 and R5.3 simultaneously, asserting both
appear in the error.

**Rationale**: `nooma doctor`'s purpose is to make the binary feel cared for. A doctor that
reports one problem per run makes the user iterate.

---

## 6. Vault resolution

### R6.1 — Four ordered steps, the third an upward search to the filesystem root

**MUST**: resolution tries, in order:

1. an explicit path argument
2. `$NOOMA_VAULT`
3. an upward search, starting at the current working directory and walking up one directory at a
   time to the filesystem root. At each directory visited, in this order:
   - **3a** — that directory *is* a vault (it satisfies R6.5's predicate)
   - **3b** — that directory *contains* exactly one usable `*.nooma` directory (R6.2)

   The first hit, at any level, wins — the nearest vault to the cwd wins. There is no `$HOME`
   ceiling or other stopping point short of the filesystem root: any ceiling is a rule the user
   would have to memorise, and the model this step follows (git's upward search from the cwd) has
   none either.
4. exactly one `*.nooma` directory inside `~/.nooma/`

The first step that yields a vault wins. Within step 3, 3a wins over 3b at the same level: if a
directory being visited is itself a vault, resolution does not also look inside it for another
one. Because the ascent visits the cwd first and moves outward, the nearest vault — including the
cwd itself — always wins over one further up the tree.

**Verified by**: L1 tests driving each step in isolation with the earlier steps unavailable;
precedence tests where two consecutive steps, or two levels of the ascent, could both succeed,
including a cwd that is a vault *and* contains a `*.nooma` directory, and a cwd several levels
below a vault with no `*.nooma` directory at any intermediate level.

**Scenario**:
- GIVEN `$NOOMA_VAULT` set to a valid vault AND a `*.nooma` directory in the cwd
- WHEN resolution runs with no argument
- THEN the `$NOOMA_VAULT` vault is chosen, and the cwd is not consulted

**Scenario**:
- GIVEN a working directory that is itself a vault (`cd pablo.nooma`)
- WHEN `nooma status` runs with no argument and no `$NOOMA_VAULT`
- THEN that directory is the vault, by step 3a

**Scenario**:
- GIVEN `pablo.nooma/attachments/` as the current working directory, inside a vault rooted at
  `pablo.nooma/`
- WHEN `nooma status` runs with no argument and no `$NOOMA_VAULT`
- THEN `attachments/` fails both 3a and 3b (it is not a vault and contains no `*.nooma`
  directory), the ascent moves up to `pablo.nooma/`, which passes 3a, and that is the vault — the
  binary does not fall through to step 4 and silently open a different vault under `~/.nooma/`

### R6.2 — Exactly one usable candidate, or an error — evaluated at every level

**MUST**: "the search directory" means step 4's `~/.nooma/`, or — during step 3's ascent (R6.1) —
whichever single directory is currently being visited. Only a `*.nooma` directory that also
satisfies R6.5's predicate counts as a *usable* candidate; R6.7 states the one name this excludes
outright regardless of the predicate.

**MUST**: when a search directory contains exactly one usable candidate, it is the vault.

**MUST**: when a search directory contains two or more usable candidates, resolution fails
immediately with an error listing every one of them by name and showing the command form that
disambiguates. The ascent does not continue past this level looking for a cleaner level higher up.

**MUST**: when a search directory contains **zero** `*.nooma`-named entries at all, step 3's
ascent continues to the parent directory; step 4, having no parent to ascend to, fails with an
error naming the directory searched and pointing at `nooma init`.

**MUST**: when a search directory contains one or more `*.nooma`-named entries and **none of them
is usable** (each fails R6.5's predicate), resolution fails immediately at that level, with an
error naming every such entry and stating what each is missing. It MUST NOT silently treat this
level as if it had zero candidates and continue the ascent, and it MUST NOT silently pick one of
the unusable entries. A broken candidate sitting between the cwd and a real vault further up the
tree must be surfaced, not stepped over.

**MUST NOT**: resolution select a candidate when more than one usable candidate exists, at any
level, by any ordering.

**Verified by**: L1 table tests for zero, one, two and three usable candidates at a single level,
asserting on the error text for the zero-at-step-4 and multiple-candidate cases; an L1 test for a
level containing one `*.nooma` entry that fails the predicate and no other entries, asserting
resolution fails naming it and what is missing rather than ascending past it; an L1 test for a cwd
several levels below a real vault with every intermediate level empty of `*.nooma` entries,
asserting the ascent reaches the vault; plus an L4 test running the binary in a directory with two
vaults.

**Scenario**:
- GIVEN `~/.nooma/` containing `pablo.nooma/` and `work.nooma/`
- WHEN `nooma serve` runs with no argument and no `$NOOMA_VAULT`
- THEN it exits non-zero, lists both candidates, and shows how to pass one explicitly
- AND neither vault's database is opened

**Scenario**:
- GIVEN a cwd containing `broken.nooma/` (a directory matching `*.nooma` with no `nooma.yml`
  inside it) and nothing else matching `*.nooma`
- WHEN resolution runs with no argument and no `$NOOMA_VAULT`
- THEN it fails at that level, naming `broken.nooma` and stating that `nooma.yml` is missing
- AND it does not silently ascend past `broken.nooma` in search of a vault higher up

**Rationale**: the obvious implementation picks the alphabetically first match and reports
nothing. Opening the wrong brain is the worst failure this component can produce, and it would be
silent. Silently skipping a broken candidate and continuing the search is the same failure by a
different door: it would let the ascent step over an obviously-intended vault (a typo'd
`nooma.yml`, or a directory mid-`init`) and resolve instead against an unrelated vault further up
the tree, without ever telling the user their intended vault has a problem.

### R6.3 — Only a directory counts as a candidate

**MUST**: a *file* named `something.nooma` is not a candidate. Only directories are.

**Verified by**: an L1 test with a file named `decoy.nooma` beside a directory `real.nooma`,
asserting the directory is chosen and the count is one, not two.

### R6.4 — An explicitly named path is used as given, with no globbing

**MUST**: steps 1 and 2 take the path as the vault itself. If it does not exist or is not a
vault, resolution fails naming that path. Resolution MUST NOT fall through to steps 3 or 4.

**MUST**: the path may be relative or absolute. A relative path resolves against the current
working directory, so `nooma serve pablo.nooma` from `/home/pablo` and
`nooma serve /home/pablo/pablo.nooma` from anywhere name the same vault.

**MUST**: the resolved vault path is absolute by the time it reaches `internal/store/sqlite.Open`,
which rejects a relative path (`ErrRelativeDBPath`).

**Verified by**: L1 tests with a nonexistent path and with a path to a directory that is not a
vault, both asserting the error names the given path and that no other location was searched; plus
L1 tests that a relative and an equivalent absolute argument resolve to the same absolute vault
path, and an L4 test invoking a command with a relative vault argument.

**Rationale**: falling through would mean a typo in `$NOOMA_VAULT` silently opens a different
brain — R6.2's failure mode by another route. The relative-path clause is stated because
`binary <path>` is the form every user already knows; without it the common invocation would work
only by accident of implementation rather than by contract.

### R6.5 — "Is a vault" is a defined, testable predicate

**MUST**: the predicate that distinguishes a vault from an ordinary directory is stated in the
design and tested. A directory that satisfies it partially (for example, `nooma.db` present and
`nooma.yml` absent) produces a specific error naming what is missing, not a generic failure.

**Verified by**: L1 tests over the partial-vault cases.

**Note**: this predicate does double duty — it is what step 3a tests to decide whether a directory
visited during the ascent is itself a vault.

**Note**: the partial-vault diagnostic probes the *default* database location (`./nooma.db`) only
— see design.md D8. A vault whose `database.path` is customised does not receive this specific
diagnostic; it is reported simply as not-a-vault (missing `nooma.yml`).

### R6.6 — Resolution never consults the executable's directory

**MUST NOT**: resolution look for a vault beside the `nooma` binary, at any step.

**Verified by**: an L1 test asserting that a `*.nooma` directory placed beside the test binary is
not resolved when steps 1, 2, 3 and 4 all fail; plus an L2 tree-scan conformance test (D16) that
fails if `os.Executable` is referenced by any non-test `.go` file under `internal/config/`, naming
the file and line, and that asserts it found files to scan before asserting the property.

**MUST NOT**: this requirement be enforced by adding a `forbidigo` exclusion rule to
`.golangci.yml`. Measured against the pinned `golangci-lint v2.12.2`, `exclusions.rules` entries OR
together, so a second rule scoped to `internal/config/` suppresses the existing `internal/core/`
rule as well — silently disabling the clock and environment enforcement of non-negotiable #3 while
appearing to add a gate. See design D16 for the measurement and the rejected alternatives.

**Rationale**: `docs/01-architecture.md`'s original step 3 was "vault next to the executable
(portable mode)". It is removed, and R1.2's doc correction removes it from the document, for three
reasons:

1. No conventional CLI resolves *data* relative to `argv[0]`. Git searches upward from the working
   directory; `kubectl`, `docker` and `psql` read `$HOME` or take a flag. Executable-relative data
   resolution is a portable-Windows-application convention, not a Unix one.
2. It reintroduces the exact failure R6.2 exists to prevent. A stray `test.nooma` in
   `/usr/local/bin/` becomes the default vault — and far more likely, a contributor's throwaway
   vault sitting next to the `go build` output in the repository root becomes the default vault
   during development.
3. It does not mean what the user thinks when a symlink is involved. `os.Executable` returns the
   *resolved* path, so with `~/.local/bin/nooma -> /opt/nooma/nooma` the search directory is
   `/opt/nooma/` — a path the user never typed and has no reason to suspect.

R6.1 implements reason 1 literally, not just by analogy: step 3 is git's own upward-search model —
the cwd first, then each parent in turn, all the way to the filesystem root, with no `$HOME`
ceiling — not a single check of the cwd and its immediate children.

Portable mode loses nothing: on removable media the invocation is `cd /media/usb && ./nooma serve`,
so step 3b already finds the vault beside the binary because it is also beside the cwd, at the
very first level the ascent tests. A Windows double-click sets the working directory to the
executable's folder, so that case is covered too.

### R6.7 — The literal name `.nooma` is never a step-3 candidate

**MUST**: during step 3's ascent, a directory entry whose full name is exactly `.nooma` (the
glob's `*` wildcard matching an empty sequence) MUST NOT be treated as a `*.nooma` candidate,
usable or not, at any level. It is excluded from the candidate set entirely, not merely filtered
out as unusable.

**MUST**: this exclusion applies only to the literal name `.nooma`. A name such as `pablo.nooma`
is unaffected and is evaluated normally.

**Verified by**: an L1 test with a directory named exactly `.nooma` present at a level the ascent
passes through (for example, at `$HOME`, on the way from a cwd several levels below it up toward
the filesystem root), asserting that level is treated as if it had zero `*.nooma` candidates — the
ascent continues past it rather than stopping with an error about it — and that resolution
proceeds to step 4 normally if the ascent reaches the root with nothing found.

**Rationale**: Go's glob for `*.nooma` matches `.nooma` itself, because `*` matches the empty
string. `~/.nooma/` is the container step 4 opens, not a vault — it holds vaults, it is not one,
and it has no `nooma.yml` of its own. Without this exclusion, R6.2's "an unusable candidate must be
surfaced, not skipped" rule would fire the moment the ascent reached `$HOME`: `.nooma` would match
the glob, fail R6.5's predicate (no `nooma.yml` at its own root), and the ascent would stop with an
error calling `~/.nooma` a broken vault — wrong on its own terms (it is not a vault, broken or
otherwise), and worse, it would pre-empt step 4 from ever running, since step 3 would already have
failed by the time step 4 would otherwise begin. Excluding the literal name outright avoids both:
the ascent passes over it exactly as if it were not there, and step 4 remains the only step that
opens `~/.nooma/`.

---

## 7. Vault creation: `nooma init`

### R7.1 — `init` creates a complete vault

**MUST**: `nooma init` produces a directory containing `nooma.db` migrated to the current
`user_version`, a `nooma.yml` valid under §3 and §5, a `.env` skeleton, and the directories
`attachments/`, `derived/` and `logs/`, per `docs/01-architecture.md` §"Vault structure".

**Verified by**: an L4 test running `init` into a temporary directory and asserting every entry
exists; plus an L3 test asserting the database's `user_version` equals the migration count.

**Scenario**:
- GIVEN an empty directory
- WHEN `nooma init` runs against it
- THEN a vault exists that `nooma status` reports on without error

### R7.1b — `init`'s target argument is optional, and its default is the installed-mode location

**MUST**: `init` accepts an optional target path. With a path, that path is the target, relative to
the cwd if relative (R6.4's rule, applied identically).

**MUST**: with **no** argument, the target is `~/.nooma/<username>.nooma`, where `<username>` is the
current OS user — the installed-mode layout `docs/01-architecture.md` depicts as
`~/.nooma/pablo.nooma/`. `init` creates `~/.nooma/` if it does not exist.

**MUST**: `init` prints the absolute path of the vault it created, on success, on stdout.

**MUST**: with no argument, `init` refuses when `~/.nooma/` already contains a usable vault, naming
it and pointing at the explicit-path form.

**MUST NOT**: a bare `nooma init` write vault contents into the current working directory.

**Verified by**: L4 tests for all four cases — bare `init` with an empty `~/.nooma/` (creates
`<username>.nooma` and prints its path), bare `init` with a vault already present (exits non-zero,
names it), `init <relative-path>`, and `init <absolute-path>` — each with `$HOME` pointed at a
temporary directory.

**Scenario**:
- GIVEN a machine with no vault anywhere
- WHEN the user runs `nooma init && nooma serve` from any working directory
- THEN `init` creates `~/.nooma/<username>.nooma`, prints that path
- AND `serve` resolves it through step 4 and starts

**Rationale**: this is the proposal's own demo (`nooma init && nooma serve`, §2), and until now
nothing in this spec said what a bare `init` targets — R10.4 lists `serve`, `status` and `doctor` as
taking an optional vault argument and omits `init` entirely, so the CLI dispatch table (D3) could
not be built from the contract.

The default is the home location rather than the cwd for one reason: a bare command that writes
`nooma.db`, `nooma.yml`, `.env`, `attachments/`, `derived/` and `logs/` into whatever directory the
user happens to be standing in is a command that will one day be run in `$HOME`, or in a source
repository. Creating a named directory in a known place is recoverable and predictable; scattering
six entries into an arbitrary cwd is neither. The refusal on an existing vault keeps step 4's
exactly-one-or-error contract (R6.2) satisfiable — a second bare `init` would otherwise make the
default location permanently ambiguous.

### R7.2 — `init` is non-interactive when told to be

**MUST**: `init` supports a fully non-interactive invocation that requires no TTY and no input.

**MUST**: the interactive wizard is a prompt layer over that same path; it MUST NOT be able to
produce a vault the non-interactive path cannot.

**Verified by**: the L4 test of R7.1 runs the non-interactive path with stdin closed; an L1 test
asserts the wizard's collected answers are the same input type the non-interactive path takes.

**Rationale**: the interactive path is the one L4 cannot drive, so it is the one that rots. Making
it a thin layer over the tested path bounds the damage.

### R7.3 — `init` never overwrites

**MUST**: running `init` against a directory that already contains a vault fails without
modifying anything.

**MUST NOT**: any part of `init` truncate or replace an existing `nooma.db`, `nooma.yml` or
`.env`.

**Verified by**: an L4 test that runs `init` twice, asserting the second run exits non-zero and
that the file contents and modification times from the first run are unchanged.

**Scenario**:
- GIVEN a vault created an hour ago with real data
- WHEN the user runs `nooma init` in the same place by mistake
- THEN nothing is written and the message says a vault already exists

**Rationale**: non-negotiable #6 — nothing is deleted in the vault. An `init` that overwrites is
a delete with better manners.

### R7.4 — A failed `init` leaves no half-vault

**MUST**: if any step of `init` fails, the directory it was creating is removed, or the failure
message states exactly what exists and what does not.

**Verified by**: an L3 or L4 test injecting a failure after the directory is created but before
migrations complete, asserting the resulting state matches one of the two allowed outcomes.

### R7.5 — `init` refuses when the target exists as a non-directory

**MUST**: if the target path exists and is a plain file, `init` fails without deleting or
truncating it, naming the existing file.

**MUST**: if the target path exists and is a symlink — to a file, to a directory, or dangling —
`init` fails without deleting, following, or writing through it, naming the symlink.

**MUST NOT**: `os.ReadDir` or `os.Remove` be called against the target before its type is
confirmed to be a plain directory.

**Verified by**: an L3 or L4 test creating a plain file at the target path, then running `init`
against it, asserting the file is unmodified (same content, same modification time) and `init`
exits non-zero naming it; and the same for a symlink pointing at an empty directory, asserting the
real directory the symlink points to still exists afterward, unmodified, and the symlink itself is
untouched.

**Scenario**:
- GIVEN `touch pablo.nooma` (a plain file, not a directory)
- WHEN `nooma init pablo.nooma` runs
- THEN it exits non-zero, the file `pablo.nooma` still exists with its original content, and no
  vault is created

**Rationale**: non-negotiable #6 ("nothing is deleted in the vault") applies one level up here, at
the vault's own root path. `os.ReadDir` on a plain file returns an error, not an error-free empty
listing — an emptiness check written as `len(entries) == 0` without also requiring `err == nil`
misclassifies a stray file as "empty", and `os.Remove` would then delete it. A symlink to an empty
directory has the same defect from the opposite direction: `os.Stat`-style emptiness checks follow
the link and report the real directory as empty, while `os.Remove` unlinks only the symlink,
orphaning the directory it pointed to.

### R7.6 — The temporary build directory's name is collision-resistant

**MUST**: the temporary directory `init` builds the vault into (D12) has a name built from the
process PID **and** a random component — a PID alone is not sufficient, because two `init`
invocations racing under a recycled or shared PID must not build into the same temporary
directory.

**Verified by**: reading the temporary-directory name construction; an L1 test asserting two calls
to the name-generation function within the same process produce distinct names.

**Rationale**: PID reuse is real over a long-running host, and a PID-only suffix would let two
concurrent `init` invocations collide and corrupt each other's partial vault before either reaches
the rename step.

---

## 8. The single-writer lock

`docs/03-data-model.md` §"Operational properties of the vault": *the binary takes a lockfile in
the vault on startup (`nooma.lock` with PID). A second `nooma serve` over the same vault fails
clearly, it does not corrupt.*

### R8.1 — A writer takes `nooma.lock` and holds it for its lifetime

**MUST**: `serve` acquires the lock before opening the database for writing and releases it on
exit, including on `SIGINT` and `SIGTERM`.

**Verified by**: an L4 test starting `serve`, asserting the lock is held, signalling it, and
asserting the lock is released.

### R8.2 — A second writer fails, naming the holder

**MUST**: a second writer on a held vault exits non-zero with a message containing the holding
process's PID.

**MUST NOT**: the second writer open the database, apply a migration, or modify any file in the
vault.

**Verified by**: an L3 test with a real second process (precedent:
`internal/store/sqlite/migrate_race_integration_test.go`), asserting the exit status, the presence
of the PID in the message, and that the database's modification time is unchanged.

**Scenario**:
- GIVEN `nooma serve` running against `pablo.nooma`
- WHEN a second `nooma serve` starts against the same vault
- THEN it exits non-zero, states the vault is in use, and names the PID holding it

### R8.3 — A crashed holder does not make the vault permanently unusable

**MUST**: after a holder terminates without releasing the lock (`SIGKILL`, power loss), a
subsequent writer can acquire the lock — either because the mechanism releases it on process
death, or because staleness is detected and reported.

**MUST NOT**: recovery require the user to delete a file by hand as the documented procedure.

**Verified by**: an L3 test that starts a real child process holding the lock, `SIGKILL`s it, and
asserts a new acquisition succeeds.

**Scenario**:
- GIVEN a `serve` process killed with `SIGKILL` while holding the lock
- WHEN `nooma serve` is started again on that vault
- THEN it acquires the lock and starts

**Rationale**: this is the requirement that eliminates a naive PID file, which survives its
process and would make a crash a manual-intervention event.

### R8.4 — Readers do not take the write lock

**MUST**: `status` and `doctor` operate on a vault held by a writer, and report the holder.

**MUST NOT**: either command acquire the write lock, block on it, or fail because of it.

**MUST**: the PID and its terminator are written to the lock file's PID region in a single write
operation. There is no separate zeroing or truncation pass that a concurrent, lock-free
`ReadHolder` could observe mid-sequence and report as "no holder" while the lock is genuinely
held.

**Verified by**: an L4 test running `status` and `doctor` while `serve` holds the lock, asserting
exit status zero and that the reported holder matches the running PID; plus an L3 test that calls
`ReadHolder` repeatedly and concurrently with `Acquire`, asserting every observed result is either
the previous holder's PID or the new holder's PID, and never an intermediate or empty read — a
regression guard against a future two-write implementation.

**Scenario**:
- GIVEN `nooma serve` running and holding the lock
- WHEN the user runs `nooma status`
- THEN it succeeds and reports the vault as in use by that PID

### R8.5 — The lock lives in `internal/store` and widens its surface deliberately, after a prerequisite golden fix lands first

**MUST**: the lock is implemented under `internal/store/**` (per `docs/06-harness.md` §1's
layout, which places the lockfile there) and `testdata/schema/store_api.golden` is regenerated in
the same PR.

**MUST**: before that PR, a separate, preceding PR extends the golden-generation code that
produces `store_api.golden` to render exported `var` and `const` declarations, not only `func`,
method and `type` declarations, and regenerates the golden. This closes a blind spot found during
review: `test/conformance/store_api_test.go:158-160` currently drops every non-`TYPE` `GenDecl`,
so an exported sentinel error such as `ErrVaultInUse` — a `var` — would widen the store surface
invisibly to the golden.

**MUST**: that preceding PR's regenerated golden diff contains exactly one new thing —
`ErrRelativeDBPath` (`internal/store/sqlite/dsn.go:15`), an exported `var` that already exists
today and was already invisible to the golden for the same reason. It is reviewed there as the
pre-existing symbol it is, precisely because widening the renderer surfaces every previously-blind
`var`/`const` at once, not only the one the lock is adding.

**MUST NOT**: the lock's exported surface provide any way to read or write a domain row.

**Verified by**: `TestHarness_StoreAPIUnchanged` passing, in two stages that must be read together
rather than as one claim: the preceding PR's regenerated golden diff contains only
`ErrRelativeDBPath` appearing; this PR's regenerated golden diff, applied on top of that
already-merged PR, then contains only the lock's own declarations, including `ErrVaultInUse`. A
single regeneration performed before the renderer-widening PR merges would show both symbols at
once and cannot satisfy this requirement — the ordering is load-bearing, not incidental.

---

## 9. The config↔doc gate

### R9.1 — The documented config example decodes into the config struct

**MUST**: an L2 conformance test extracts the `yaml` block under `docs/01-architecture.md`
§"Configuration — `nooma.yml`" and decodes it into the config type under the same strict rules
the loader uses (R3.2).

**MUST**: the test fails when a key is documented but has no field, and when a field exists but is
undocumented.

**MUST**: for a map-typed configuration field (`Config.Providers`, `Config.Tasks` — `map[string]X`
where the keys are user-chosen names, not schema), the schema side recurses into the map's *value*
type (`Provider`, `TaskBinding`) to obtain its field names, and never treats the map's own keys as
schema.

**MUST**: for that same map-typed field, the document side **unions** the field names observed
across every entry present in that map section of the document before comparing against the value
type's schema. Completeness ("every field the struct has appears in the document") is evaluated
against this union, and MUST NOT be required of any single entry — doc 01's actual `providers:`
block has four entries using disjoint field subsets (`claude_cloud`/`claude_haiku`: `type`,
`api_key_env`, `model`; `local_llama`: `type`, `endpoint`, `model`; `whisper_local`: `type`,
`binary_path`, `model_path`), and no single entry contains every `Provider` field by design — a
per-entry completeness check could never pass on this document, or any realistic one.

**MUST NOT**: the gate validate values — it decodes the document and compares key schemas only,
never checking a value's business meaning (for example, that a task's `provider` name exists in
the declared `providers:` map). `docs/01-architecture.md`'s tasks block contains the literal
placeholder `embedding: { provider: ... }`, and `...` decodes cleanly to the Go string `"..."`
under the loader's `Strict()` rules — it would fail any validator checking that value, even though
it is exactly what a documentation placeholder should look like.

**Verified by**: the test observed failing in both directions before it passes — once by removing
a key from the doc, once by adding a field to the struct; plus a dedicated test case built from
doc 01's actual heterogeneous `providers:` block, asserting the gate passes even though no single
entry uses every `Provider` field, because the union across all four entries does.

**Scenario**:
- GIVEN a developer adds `server.read_timeout` to the config struct
- WHEN CI runs
- THEN the gate fails, naming the undocumented field

**Scenario**:
- GIVEN doc 01's `providers:` block, where `claude_cloud` uses `type`/`api_key_env`/`model` and
  `local_llama` uses `type`/`endpoint`/`model` — neither entry alone covers every `Provider` field
- WHEN the gate runs
- THEN it passes, because the union of fields observed across all `providers:` entries covers
  every `Provider` field, and no entry is checked for completeness on its own

### R9.2 — The extraction is strict about which block it reads

**MUST**: the extractor identifies its block unambiguously and fails loudly if the document
contains zero or more than one candidate block in that section.

**MUST NOT**: the extractor pick the first matching fence.

**Verified by**: unit tests over synthetic documents with zero, one and two candidate blocks.

**Rationale**: an ad hoc fence match reintroduced a silent-first-pick defect during
`complete-harness` — inside the very file that added a strict extractor to prevent it.
`test/support/schema/markdown.go`'s extractor does **not** solve this: it is hardcoded to
```` ```sql ```` fences, deliberately collects every such fence across the whole document (doc 03
legitimately has many `CREATE` blocks), and has no section scoping and no arity check. The real
"exactly one, else error" precedent is `test/support/goldenset/markdown.go`'s
`ExtractJSONFence` — JSON-specific and also unscoped to a section, but shaped the way this
requirement needs. This gate's extractor is **new code**, modeled on `ExtractJSONFence`'s
exactly-one-or-error shape, extended with section scoping and a `yaml` tag (see design.md §6).

### R9.3 — The gate runs on every PR

**MUST**: the gate is part of the untagged L2 suite, so `make test` and CI's test job run it with
no new job and no new tag.

**Verified by**: `go test ./test/conformance/` executes it.

---

## 10. CLI surface

### R10.1 — Five commands exist and no more

**MUST**: `nooma` dispatches `init`, `serve`, `status`, `doctor` and `version`.

**MUST NOT**: `consolidate`, `reindex`, `export` or `import` be present, even as stubs that print
"not implemented".

**Verified by**: an L4 test asserting each of the five exits as specified, and that a
not-yet-implemented command name exits non-zero with an unknown-command error.

**Rationale**: a stub that accepts the invocation teaches the user the command exists. An unknown
command is the honest answer until the command works.

### R10.2 — `version` behavior is unchanged

**MUST**: `nooma version` keeps printing `nooma <version> (<revision>)` and
`test/e2e/version_test.go` continues to pass unmodified.

**Verified by**: the existing L4 test, unedited.

### R10.3 — No arguments prints usage listing the real commands

**MUST**: `nooma` with no arguments prints a usage message naming exactly the commands of R10.1,
and exits zero.

**Verified by**: an L4 test asserting exit zero and one line per command.

### R10.4 — Every command that touches a vault accepts one the same way

**MUST**: `serve`, `status` and `doctor` each accept an optional vault path argument, and in its
absence use resolution §6 identically.

**MUST**: `init` also accepts an optional path argument, resolved relative to the cwd by the same
rule, but its no-argument behavior is **not** resolution §6 — it is R7.1b's fixed default, because
`init` creates a vault rather than finding one. Resolving a vault that does not exist yet is not a
meaningful operation, which is why `init` is the one vault-touching command that does not share §6.

**Verified by**: L4 tests running all three with an explicit path and with `$NOOMA_VAULT`.

### R10.5 — Errors go to stderr and the exit code is non-zero

**MUST**: every failure path writes its message to stderr and exits non-zero. Success output goes
to stdout.

**Verified by**: L4 tests capturing the two streams separately for one success and one failure per
command.

---

## 11. `nooma serve`

### R11.1 — Binds loopback by default and serves two endpoints

**MUST**: with no `server.bind` configured, `serve` listens on `127.0.0.1` at
`server.http_port`, serving an API hello response and a `/ui` placeholder page.

**Verified by**: an L4 test that starts `serve` on an ephemeral port, requests both endpoints, and
asserts a 2xx with a non-empty body from each.

### R11.2 — A non-loopback bind without `server.auth_token_env` refuses to start

**MUST**: when `server.bind` is not a loopback address and `server.auth_token_env` is absent, or
names an unset variable, `serve` exits non-zero **without listening on any socket**.

**MUST**: the message states that a non-loopback bind requires an auth token, per ADR-0007.

**Verified by**: L1 tests over the decision (loopback/non-loopback × token present/absent), plus
an L4 test asserting the process exits non-zero and that nothing is listening on the configured
address afterwards.

**Scenario**:
- GIVEN `server: { bind: 0.0.0.0, http_port: 7777 }` and no `auth_token_env`
- WHEN `nooma serve` runs
- THEN it exits non-zero before binding, naming ADR-0007's rule
- AND no socket was opened at any point

**Rationale**: ADR-0007 — the safety of the default is structural, not a warning. A server that
starts and *then* complains has already exposed the port.

### R11.3 — Loopback detection is by address semantics, not string matching

**MUST**: the loopback decision covers at minimum `127.0.0.1`, `::1`, `localhost`, the whole
`127.0.0.0/8` range, and treats `0.0.0.0` and `::` as non-loopback.

**MUST NOT**: the decision be a prefix or substring comparison on the configured string.

**Verified by**: an L1 table test over those addresses plus adversarial cases (`127.0.0.1.evil`,
`0127.0.0.1`, an address with a port already attached).

**Rationale**: this is a security boundary decided by parsing. A string test that accepts
`127.0.0.1.evil` as loopback disables ADR-0007 silently.

### R11.4 — The token, when required, is not embedded in the config file

**MUST**: `server.auth_token_env` names an environment variable. M0 verifies the variable is set;
enforcement of the token on requests arrives with the HTTP API in M1/M4.

**MUST NOT**: M0 accept a literal token key in the config (R4.1).

**Verified by**: the R4.1 test and the R11.2 tests.

### R11.5 — `serve` shuts down cleanly

**MUST**: on `SIGINT` or `SIGTERM`, `serve` stops the listener, releases the lock (R8.1), closes
the database, and exits zero.

**Verified by**: an L4 test signalling the process and asserting exit zero and a released lock.

---

## 12. `nooma status`

### R12.1 — What `status` reports

**MUST**: `status` reports the resolved vault path, the schema `user_version`, whether the vault
is currently held and by which PID, the size of the database file on disk, and a summary of the
effective configuration (bind, port, whether the UI is enabled, which channels are configured).

**Verified by**: an L4 test asserting each element is present in the output for a known vault.

### R12.2 — `status` reads no domain row

**MUST NOT**: `status` execute a query against `units`, `relations`, `triggers`, `beliefs`,
`decision_log`, or any other domain table.

**Verified by**: the scope boundary's three existing layers — the unexported handle, the
`sqlite-containment` depguard rule (`cmd/**` cannot import `database/sql`), and
`testdata/schema/store_api.golden`, which would have to grow a row-reading declaration for this to
become possible.

**Rationale**: `docs/01-architecture.md` promises "last consolidation" here. That is a domain row,
and the store surface exists to make it unreachable until M1/M2. R1.4 dates the doc's claim rather
than weakening the boundary.

### R12.3 — `status` works on a vault in use

Covered by R8.4.

---

## 13. `nooma doctor`

### R13.1 — What `doctor` checks

**MUST**: `doctor` reports, each as an individually identifiable result: configuration validity
(§3, §5), read/write permissions on the vault directory and its subdirectories,
`PRAGMA integrity_check`, the schema `user_version` against the expected migration count, and the
effective bind together with whether it is exposed.

**Verified by**: an L4 test on a healthy vault asserting every check is reported; plus per-check
L3 tests that break one thing at a time (an unreadable directory, a config violation, a
`user_version` mismatch).

### R13.2 — `doctor` reports every failure, not the first

**MUST**: `doctor` runs every check it can and reports all results. A failing check does not
abort the remaining ones.

**Verified by**: an L3 or L4 test on a vault with two independent problems, asserting both appear.

### R13.3 — `doctor`'s exit code distinguishes healthy from not

**MUST**: `doctor` exits zero when every check passes and non-zero when any check fails, so it is
usable in a script.

**Verified by**: L4 tests on a healthy and an unhealthy vault.

### R13.4 — What `doctor` does not check in M0

**MUST NOT**: M0's `doctor` attempt provider connectivity, model availability, or hardware
assessment.

**Rationale**: providers arrive in M1; "minimum hardware" is an open dated decision due before M6
(`docs/04-decisions.md`). A check that cannot be implemented honestly is worse than an absent one,
because its passing means nothing.

**Verified by**: absence — no network call exists in the command's code path, consistent with
non-negotiable #5 (no test touches the network).

### R13.5 — `integrity_check` is reached through the store's surface, as `(*Vault).IntegrityCheck`

**MUST**: the integrity check is exposed by `internal/store/sqlite` as a method named
`IntegrityCheck` on `*Vault`, and `store_api.golden` is regenerated in the same PR (as in R8.5),
because `cmd/**` cannot import `database/sql` under the `sqlite-containment` rule.

**MUST NOT**: the new method be named or wired to `(*Vault).Check` — that method already exists at
`internal/store/sqlite/open.go:192`, is already present in `store_api.golden`, and is an unrelated
FTS5-registration probe. `IntegrityCheck` is a distinct, additional method.

**Verified by**: `TestHarness_StoreAPIUnchanged` against the regenerated golden; the golden diff
shows `IntegrityCheck` as a new entry, and `Check`'s existing entry is unchanged.

---

## 14. Boundaries this change must not cross

### R14.1 — No domain symbol is created

**MUST NOT**: this change create `unit.Status`, `unit.AllStatuses`, `ports.UnitRepo`,
`recall.VectorQuery` or `recall.VectorIndex`.

**Verified by**: `make pending-red` stays green throughout the chain — the tagged conformance
package still fails to compile, reporting all five as undefined.

**Scenario**:
- GIVEN the full M0 chain merged
- WHEN `sh scripts/pending-red.sh` runs
- THEN it passes, and `test/conformance/pending_symbols.txt` is unmodified

### R14.2 — The schema is not extended

**MUST NOT**: this change add a migration, alter an existing one, or change
`docs/03-data-model.md`'s schema sections.

**Verified by**: `make schema-golden-clean` leaves a clean tree; `git diff` over
`internal/store/sqlite/migrations/` is empty across the chain.

### R14.3 — `internal/core` is not touched

**MUST NOT**: this change add or modify a file under `internal/core/**`.

**Consequence**: the `docs-sync.yml` gate does not fire, and no `no-spec-change` label is needed
on any PR in the chain.

**Verified by**: `git diff --name-only` over the chain contains no `internal/core/` path.

### R14.4 — The I01 structural scan keeps passing

**MUST NOT**: any source line added by this change contain both the literal `"focus"` and the
substring `Status`.

**Verified by**: `TestI01FocusNeverPersisted`'s tree scan, part of the untagged suite.

**Rationale**: the scan is deliberately coarse. M0 adds a status-reporting command, so the
collision is plausible: a config or CLI line that pairs the word focus with something named
Status would trip it. Naming around it is cheaper than arguing with it.

---

## 15. Test levels

### R15.1 — Level assignment

**MUST**: config decoding, validation, vault resolution, loopback detection and path resolution
are **L1** — pure functions over in-memory or temp-dir inputs, no database, no network, no
process.

**MUST**: the lock under contention, `integrity_check`, and any behavior requiring a real second
process are **L3** (`integration` tag).

**MUST**: the lock's L3 tests (R8.2, R8.3) run on both `ubuntu-latest` and `windows-latest` in CI.
The e2e (L4) job's Windows runner (D6) does not exercise the lock — the L4 job's scope is every
command's *observable contract* (§10-§13), which excludes the lock's own contention and
crash-recovery behavior — so the L3 job is the one that needs the Windows leg for `LockFileEx` to
be genuinely exercised rather than only cross-compiled.

**MUST**: every command's observable contract is **L4** (`e2e` tag), driving the compiled binary.

**MUST**: the config↔doc gate is **L2** (untagged conformance).

**Verified by**: file placement and build tags, checked by `docs/06-harness.md` §3's taxonomy; the
`integration` job's matrix in `.github/workflows/ci.yml` includes `windows-latest`.

### R15.2 — No test touches the network

**MUST NOT**: any test in this change make a network call. `serve`'s L4 tests bind an ephemeral
port on loopback, which is not a network call under `docs/06-harness.md` §3's meaning, and MUST be
stated as such where it appears.

**Verified by**: review; no test imports a client for an external service.

### R15.3 — Every new test is observed failing for the right reason first

**MUST**: each requirement's test is written before its implementation and observed failing with
the expected message, per non-negotiable #4 and strict TDD mode.

**MUST NOT**: a failing test be weakened to pass. The two legitimate exits are fixing the code or
changing the governing document and its ADR in the same PR.

**Verified by**: the commit sequence within each PR — a work-unit commit contains the test and the
code that satisfies it, and the PR description records what was observed failing.
