# docker-bake.hcl — mcp-go-gen container build matrix.
#
# Phase 1 stub per IMPL-0001: the CI workflow at .github/workflows/ci.yml
# references this file and a `ci` target. This stub builds the Phase 1
# Dockerfile for linux/amd64 and linux/arm64; production hardening lands
# in Phase 7.
#
# Usage:
#   docker buildx bake ci                 # local build, both arches
#   docker buildx bake ci --set *.output=type=registry   # push to IMAGE_REPO

variable "IMAGE_REPO" {
  default = "ghcr.io/donaldgifford/mcp-go-gen"
}

variable "IMAGE_TAG" {
  default = "dev"
}

variable "VERSION" {
  default = "dev"
}

variable "COMMIT" {
  default = "none"
}

group "default" {
  targets = ["ci"]
}

target "ci" {
  context    = "."
  dockerfile = "Dockerfile"
  platforms  = ["linux/amd64", "linux/arm64"]
  args = {
    VERSION = "${VERSION}"
    COMMIT  = "${COMMIT}"
  }
  tags = [
    "${IMAGE_REPO}:${IMAGE_TAG}",
  ]
}

# docker-metadata-action is the default target name that
# docker/metadata-action injects tags/labels into via its bake-file
# outputs. The release target inherits from it so the multi-arch
# image gets the version/major.minor/latest tag set computed by the
# workflow. Empty by design — fields come from the metadata file.
target "docker-metadata-action" {}

target "release" {
  inherits   = ["docker-metadata-action"]
  context    = "."
  dockerfile = "Dockerfile"
  platforms  = ["linux/amd64", "linux/arm64"]
  args = {
    VERSION = "${VERSION}"
    COMMIT  = "${COMMIT}"
  }
  output = ["type=registry"]
}
