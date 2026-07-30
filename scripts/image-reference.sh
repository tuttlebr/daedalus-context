#!/usr/bin/env bash

# Docker tags cannot contain SemVer build metadata ("+..."), so release image
# references support MAJOR.MINOR.PATCH, an optional leading "v", and an
# optional SemVer prerelease suffix.
SEMVER_IMAGE_REF_PATTERN='^[^[:space:]@]+:v?[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$'

require_semver_image_ref() {
  local variable_name="$1"
  local image_ref="$2"

  if [[ ! "${image_ref}" =~ ${SEMVER_IMAGE_REF_PATTERN} ]]; then
    printf '%s must use a SemVer image tag (repository:MAJOR.MINOR.PATCH); got %q.\n' \
      "${variable_name}" "${image_ref}" >&2
    return 1
  fi
}

semver_image_tag() {
  local image_ref="$1"
  local tag="${image_ref##*:}"

  printf '%s\n' "${tag#v}"
}
