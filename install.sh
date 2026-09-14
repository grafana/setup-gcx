#!/usr/bin/env bash
#
# install.sh — resolve, download, verify, and install the gcx CLI.
# Invoked by action.yml as a composite-action step.
#
# Expects env:
#   INPUT_VERSION  gcx version to install ("latest" or e.g. "v1.3.0")
#   GH_TOKEN       token for GitHub API calls (release lookup)
#   RUNNER_OS      Linux | macOS | Windows      (provided by the runner)
#   RUNNER_ARCH    X64 | ARM64                  (provided by the runner)
#   RUNNER_TEMP    scratch dir                  (provided by the runner)
#   GITHUB_PATH    file to append PATH entries  (provided by the runner)
#   GITHUB_OUTPUT  file to write step outputs   (provided by the runner)

set -euo pipefail

REPO="grafana/gcx"
API="https://api.github.com/repos/${REPO}"

log()  { echo "==> $*"; }
fail() { echo "::error::$*" >&2; exit 1; }

# --- 1. Resolve version -----------------------------------------------------
# Normalize to tag form (v1.3.0) and asset form (1.3.0 — GoReleaser strips the v).
request() {
  # request <url> — GET with auth + API version headers.
  local url="$1"
  local -a auth=()
  [[ -n "${GH_TOKEN:-}" ]] && auth=(-H "Authorization: Bearer ${GH_TOKEN}")
  curl -sfL "${auth[@]}" \
    -H "Accept: application/vnd.github+json" \
    -H "X-GitHub-Api-Version: 2022-11-28" \
    "$url"
}

VERSION_INPUT="${INPUT_VERSION:-latest}"
if [[ "$VERSION_INPUT" == "latest" ]]; then
  log "Resolving latest gcx release"
  TAG="$(request "${API}/releases/latest" \
    | grep -m1 '"tag_name"' | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')"
  [[ -n "$TAG" ]] || fail "Could not resolve the latest gcx release"
else
  TAG="$VERSION_INPUT"
fi

# tag keeps the leading v; asset filenames drop it.
TAG="v${TAG#v}"
ASSET_VERSION="${TAG#v}"
log "Installing gcx ${TAG}"

# --- 2. Map runner OS/arch --> gcx asset naming -----------------------------
case "${RUNNER_OS}" in
  Linux)   GCX_OS="linux"   ; EXT="tar.gz" ; BIN="gcx"     ;;
  macOS)   GCX_OS="darwin"  ; EXT="tar.gz" ; BIN="gcx"     ;;
  Windows) GCX_OS="windows" ; EXT="zip"    ; BIN="gcx.exe" ;;
  *) fail "Unsupported runner OS: ${RUNNER_OS}" ;;
esac

case "${RUNNER_ARCH}" in
  X64)   GCX_ARCH="amd64" ;;
  ARM64) GCX_ARCH="arm64" ;;
  *) fail "Unsupported runner arch: ${RUNNER_ARCH}" ;;
esac

ARCHIVE="gcx_${ASSET_VERSION}_${GCX_OS}_${GCX_ARCH}.${EXT}"
CHECKSUMS="gcx_${ASSET_VERSION}_checksums.txt"
BASE="https://github.com/${REPO}/releases/download/${TAG}"

# --- 3. Download archive + checksums ----------------------------------------
WORK="${RUNNER_TEMP}/setup-gcx"
mkdir -p "$WORK"

log "Downloading ${ARCHIVE}"
curl -sfL -o "${WORK}/${ARCHIVE}"   "${BASE}/${ARCHIVE}"   || fail "Failed to download ${ARCHIVE}"
curl -sfL -o "${WORK}/${CHECKSUMS}" "${BASE}/${CHECKSUMS}" || fail "Failed to download ${CHECKSUMS}"

# --- 4. Verify sha256 (fail hard on mismatch) -------------------------------
log "Verifying checksum"
EXPECTED="$(grep " ${ARCHIVE}\$" "${WORK}/${CHECKSUMS}" | awk '{print $1}')"
[[ -n "$EXPECTED" ]] || fail "No checksum entry for ${ARCHIVE} in ${CHECKSUMS}"

if command -v sha256sum >/dev/null 2>&1; then
  ACTUAL="$(sha256sum "${WORK}/${ARCHIVE}" | awk '{print $1}')"
else
  ACTUAL="$(shasum -a 256 "${WORK}/${ARCHIVE}" | awk '{print $1}')"
fi

[[ "$EXPECTED" == "$ACTUAL" ]] \
  || fail "Checksum mismatch for ${ARCHIVE}: expected ${EXPECTED}, got ${ACTUAL}"

# --- 5. Extract -------------------------------------------------------------
TOOL_DIR="${WORK}/bin"
mkdir -p "$TOOL_DIR"
log "Extracting ${BIN}"
if [[ "$EXT" == "zip" ]]; then
  unzip -o -q "${WORK}/${ARCHIVE}" "${BIN}" -d "$TOOL_DIR"
else
  tar -xzf "${WORK}/${ARCHIVE}" -C "$TOOL_DIR" "${BIN}"
fi

BIN_PATH="${TOOL_DIR}/${BIN}"
[[ -f "$BIN_PATH" ]] || fail "Expected binary ${BIN} not found in ${ARCHIVE}"
chmod +x "$BIN_PATH"

# --- 6. Export PATH + outputs -----------------------------------------------
echo "$TOOL_DIR" >> "$GITHUB_PATH"
{
  echo "version=${TAG}"
  echo "path=${BIN_PATH}"
} >> "$GITHUB_OUTPUT"

log "Installed gcx ${TAG} at ${BIN_PATH}"
