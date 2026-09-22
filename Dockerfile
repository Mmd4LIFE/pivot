# Pivot, as one container.
#
# Part 12 needs this to exist so a broken Dockerfile fails a pull request
# rather than a release. Part 13 owns publishing it: multi-architecture builds,
# provenance, an SBOM and the tagging scheme all land there. What is here is
# the smallest correct image.
#
# Three stages, because the frontend toolchain has no business in the runtime
# image: Node builds the assets, Go embeds them, and the result is copied into
# a base with no shell and no package manager.

# ── The browser application ──────────────────────────────────────────────────
FROM node:22-bookworm-slim AS web

WORKDIR /src/web

# The manifests first, so a change to application code does not invalidate the
# dependency layer. `npm ci` and not `npm install`: it installs exactly the
# lockfile and fails if the two disagree, which is the behavior a build wants
# and the opposite of what a developer wants.
COPY web/package.json web/package-lock.json ./
RUN npm ci

COPY web/ ./
RUN npm run build

# ── The binary ───────────────────────────────────────────────────────────────
# Pinned to the patch, and it must match go.mod's `toolchain` line -- CI
# asserts that. An unpinned `golang:1.26` drifts, which is the same class of
# problem as building with the `go` directive: it decides on its own which
# standard library vulnerabilities the image ships with.
FROM golang:1.26.8-bookworm AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# The built assets, overwriting the committed placeholder that lets
# `go build ./...` work without Node.
COPY --from=web /src/web/dist ./web/dist

ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown

# CGO off, so the result is static and runs on a base with no libc of its own.
# That is possible at all because ADR-0003 chose modernc.org/sqlite: a cgo
# SQLite driver would have made this a distro image with a package manager in
# it.
RUN CGO_ENABLED=0 go build \
      -trimpath \
      -ldflags="-s -w \
        -X 'github.com/Mmd4LIFE/pivot/internal/version.version=${VERSION}' \
        -X 'github.com/Mmd4LIFE/pivot/internal/version.commit=${COMMIT}' \
        -X 'github.com/Mmd4LIFE/pivot/internal/version.date=${DATE}' \
        -X 'github.com/Mmd4LIFE/pivot/internal/version.builtBy=docker'" \
      -o /out/pivot ./cmd/pivot

# ── The image ────────────────────────────────────────────────────────────────
#
# Distroless static: no shell, no package manager, no libc. Nothing to exploit
# and nothing to patch, which is the right shape for something self-hosted by
# people who will not be watching a CVE feed.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/pivot /usr/local/bin/pivot

# Writable state lives here so a single volume mount covers it. The default
# SQLite path in Part 15's zero-config first run will point inside it.
WORKDIR /var/lib/pivot

# Already the base image's user; stated so that it survives a base change.
USER nonroot:nonroot

EXPOSE 8080

# No HEALTHCHECK. There is no shell and no curl to run one with, and the
# orchestrators that matter -- Compose, Kubernetes -- probe /readyz over HTTP
# themselves. A healthcheck that cannot run is worse than none, because it
# reports unhealthy forever.

ENTRYPOINT ["/usr/local/bin/pivot"]
CMD ["serve"]
