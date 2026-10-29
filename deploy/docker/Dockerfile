# syntax=docker/dockerfile:1
#
# Nooma's server image (docs/08-deploy.md). Built from the repository root:
#
#   docker compose -f deploy/docker/compose.yml -f deploy/docker/compose.build.yml up -d --build

FROM golang:1.26-bookworm AS build
WORKDIR /src

# Modules in their own layer: a source change does not download them again.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# One static build of the binary, nothing else, to fit a 1 GB droplet.
# The version is `git describe`, as in the Makefile (ADR-0015), so .git is in
# the build context; a tree without it builds as "dev". timetzdata embeds the
# zone database, so TZ works whatever the runtime image carries.
RUN --mount=type=cache,target=/root/.cache/go-build \
    git config --global --add safe.directory /src; \
    if git rev-parse --git-dir >/dev/null 2>&1; then \
      vcs=auto; version="$(git describe --tags --dirty 2>/dev/null || echo dev)"; \
    else \
      vcs=false; version=dev; \
    fi; \
    CGO_ENABLED=0 go build -trimpath -buildvcs="$vcs" -tags timetzdata \
      -ldflags "-s -w -X main.version=$version" -o /out/nooma ./cmd/nooma && \
    mkdir -p /out/data /out/backups

# distroless/static: CA certificates for the provider and Telegram, no shell,
# and the nonroot user (uid 65532).
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=build /out/nooma /usr/local/bin/nooma
COPY --from=build --chown=65532:65532 /out/data /data
COPY --from=build --chown=65532:65532 /out/backups /backups
ENV NOOMA_VAULT=/data/vault
USER 65532:65532
EXPOSE 7777
ENTRYPOINT ["nooma"]
CMD ["serve"]
