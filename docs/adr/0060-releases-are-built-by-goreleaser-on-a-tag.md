# ADR-0060 — Releases are built by goreleaser on a tag, signed keyless, and published to GHCR

- **Status**: Proposed
- **Date**: 2026-10-29
- **Supersedes**: —
- **Superseded by**: —
- **Enables**: M6 (an installable release, slice F)

## Context

M6's demo is an installable release used by a stranger without help. Until now a release was a
tag and nothing else: the only remote tag is `v0.1.0`, there is no attached binary, and the one
server deployment builds its image on the host from a clone (`docs/08-deploy.md`), which needs a
Go toolchain's worth of memory on a 1 GB droplet.

[ADR-0015](0015-versioning.md) already fixed the grammar (SemVer, `v`-prefixed, `-rc.N` before
`1.0.0`), the source of truth (the annotated tag, the GitHub Release as presentation) and the
stamp (`git describe` into `-X main.version=`). It named goreleaser as M6's tool. ADR-0013 fixed
the seven targets. What remains is how a tag becomes artifacts, how a stranger can trust them,
and where the image lives.

## Options evaluated

| Option | Pro | Con |
|---|---|---|
| **goreleaser, with its `dockers_v2` image build** | One config builds the seven binaries, archives, checksums, the signature, the Release and the multi-arch image from the same compiled binaries | One more tool, run only in CI; its config is checked here as data, not executed |
| A hand-written workflow: `go build` matrix + `gh release` + `docker/build-push-action` | No new tool | Re-implements archiving, checksums and the manifest list, and the image compiles a second binary that can differ from the archived one |
| Images built from source by buildx (today's Dockerfile, multi-platform) | No prebuilt-binary Dockerfile | Compiles three more times under emulation; the image's binary is not the Release's |

| Signing | Verdict |
|---|---|
| **cosign keyless, the workflow's GitHub OIDC identity, over `checksums.txt`** | Chosen. No key to store, rotate or leak; the signature names the workflow and the tag that produced it, in a public transparency log |
| A long-lived cosign or GPG key in a repository secret | Rejected: a secret to protect for the life of the project, for no gain over the OIDC identity |
| No signature, checksums only | Rejected: a checksum served beside the file proves integrity, not origin |

| Registry | Verdict |
|---|---|
| **GHCR, `ghcr.io/rengo/nooma`** | Chosen. Same account as the repository, pushed with the workflow's own token |
| Docker Hub | Rejected: a second account and a long-lived token secret |

## Decision

1. **A pushed `v*` tag runs `.github/workflows/release.yml`, and nothing else does.** No pull
   request, no merge, no manual dispatch. The workflow's token has exactly `contents: write`,
   `id-token: write` and `packages: write`. The tag goes on a `main` commit that already passed
   every gate, so the workflow does not re-run them.
2. **goreleaser builds ADR-0013's seven targets** with `CGO_ENABLED=0 -trimpath -tags
   timetzdata`, `linux/arm` as GOARM=7, stamped `-X main.version=<tag>`: the Makefile's stamp,
   since `git describe` on a tag commit prints the tag.
3. **Archives** are `tar.gz`, `zip` on Windows, each with `LICENSE` and `README.md`. One
   `checksums.txt` covers them, signed by `cosign sign-blob` keyless into
   `checksums.txt.sigstore.json`.
4. **The image is `ghcr.io/rengo/nooma:<version>`** (the tag without its `v`) for `linux/amd64`,
   `linux/arm64` and `linux/arm/v7`, built by `deploy/docker/Dockerfile.release` from the binaries
   already archived. Its runtime stage is the source image's: distroless `static:nonroot`, uid
   65532, `NOOMA_VAULT=/data/vault`.
5. **`:latest` moves only on a tag without a prerelease suffix.** `v1.0.0-rc.1` publishes
   `1.0.0-rc.1` and a GitHub prerelease; `latest` stays on the last final release.
6. **The build-from-source `Dockerfile` stays**, for development and for running unreleased
   `main`, behind `deploy/docker/compose.build.yml`. The default compose service pulls the image.

## Consequences

- A user downloads a binary and verifies it without trusting the download page: `cosign
  verify-blob` on the bundle, pinned to this repository's workflow identity, then `sha256sum -c`
  (the commands are in `docs/06-harness.md` §6).
- The droplet upgrades by pulling, and no longer compiles: the swap note applies to the
  source build only.
- The pipeline cannot be exercised on a pull request. `test/harness/release_test.go` holds the
  config to this ADR as data (targets, stamp, archives, signature, image tags, trigger,
  permissions); the first real run is the first tag after this merges.
- A GHCR package published by a workflow may start private; making it public is a one-time
  setting on the package.
- The image itself is not signed. Its binaries are the signed checksums' binaries, and an image
  signature is a later, separate decision.

## Reversal criteria

- goreleaser stops supporting a target, the signing flow or the image build this ADR relies on.
- The project leaves GitHub, which takes the OIDC identity and GHCR with it.
- A user needs to verify the image itself rather than the binaries inside it.
