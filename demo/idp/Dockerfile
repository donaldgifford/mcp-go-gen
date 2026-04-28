# syntax=docker/dockerfile:1.7
# demo-idp — minimal RS256 JWT issuer for IMPL-0002 Phase 5. See INV-0001
# for the rationale on hand-rolling rather than running dex/Keycloak.

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags "-s -w" -o /out/demo-idp .

FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/demo-idp /usr/local/bin/demo-idp
USER nonroot:nonroot
EXPOSE 5556
ENTRYPOINT ["/usr/local/bin/demo-idp"]
