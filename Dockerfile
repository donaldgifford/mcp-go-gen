# syntax=docker/dockerfile:1.7
#
# mcp-go-gen container image (Phase 1 stub).
#
# Phase 7 of IMPL-0001 hardens this with non-root defaults, tighter base
# pinning, and full SBOM/provenance attestations. Today's job is narrow:
# produce a runnable binary on linux/amd64 and linux/arm64 so CI's
# docker-build job stops failing on a missing bake file.

ARG GO_VERSION=1.26

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS build
WORKDIR /src

# Module warming is isolated so the layer caches across code-only changes.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .

ARG TARGETOS TARGETARCH
ARG VERSION=dev
ARG COMMIT=none
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build \
      -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
      -o /out/mcp-go-gen ./cmd/mcp-go-gen

FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/mcp-go-gen /usr/local/bin/mcp-go-gen
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/mcp-go-gen"]
