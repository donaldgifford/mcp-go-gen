# syntax=docker/dockerfile:1.7
# demo-api — minimal HTTP service backing the IMPL-0002 demo MCP server.

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY . .

ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags "-s -w" -o /out/demo-api .

FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/demo-api /usr/local/bin/demo-api
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/demo-api"]
