#!/usr/bin/env python3
#
# install.py — resolve, download, verify, and install the gcx CLI.
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

import hashlib
import json
import os
import stat
import sys
import tarfile
import urllib.error
import urllib.request
import zipfile

REPO = "grafana/gcx"
API = f"https://api.github.com/repos/{REPO}"


def request(url):
    """GET a URL with auth + API version headers, returning the response body as bytes."""
    headers = {
        "Accept": "application/vnd.github+json",
        "X-GitHub-Api-Version": "2022-11-28",
    }
    token = os.environ.get("GH_TOKEN")
    if token:
        headers["Authorization"] = f"Bearer {token}"
    req = urllib.request.Request(url, headers=headers)
    with urllib.request.urlopen(req) as resp:
        return resp.read()


def download(url, dest):
    """Download url to dest, failing hard on any error.

    No Authorization header: release assets are public, and GitHub redirects
    the download to a different host (release-assets.githubusercontent.com).
    urllib copies request headers across redirects, so attaching the token
    here would leak it to the asset host.
    """
    req = urllib.request.Request(url)
    with urllib.request.urlopen(req) as resp, open(dest, "wb") as f:
        while True:
            chunk = resp.read(65536)
            if not chunk:
                break
            f.write(chunk)


def main():
    # --- 1. Resolve version -------------------------------------------------
    # Normalize to tag form (v1.3.0) and asset form (1.3.0 — GoReleaser strips the v).
    version_input = os.environ.get("INPUT_VERSION", "latest") or "latest"
    if version_input == "latest":
        print("==> Resolving latest gcx release")
        try:
            release_json = request(f"{API}/releases/latest")
        except urllib.error.URLError:
            print("::error::Could not query the latest gcx release", file=sys.stderr)
            sys.exit(1)
        try:
            tag = json.loads(release_json)["tag_name"]
        except (ValueError, KeyError):
            print("::error::Could not resolve the latest gcx release", file=sys.stderr)
            sys.exit(1)
    else:
        tag = version_input

    # tag keeps the leading v; asset filenames drop it.
    tag = "v" + tag[1:] if tag.startswith("v") else "v" + tag
    asset_version = tag[1:]
    print(f"==> Installing gcx {tag}")

    # --- 2. Map runner OS/arch --> gcx asset naming -------------------------
    runner_os = os.environ.get("RUNNER_OS", "")
    os_map = {
        "Linux": ("linux", "tar.gz", "gcx"),
        "macOS": ("darwin", "tar.gz", "gcx"),
        "Windows": ("windows", "zip", "gcx.exe"),
    }
    if runner_os not in os_map:
        print(f"::error::Unsupported runner OS: {runner_os}", file=sys.stderr)
        sys.exit(1)
    gcx_os, ext, binary = os_map[runner_os]

    runner_arch = os.environ.get("RUNNER_ARCH", "")
    arch_map = {"X64": "amd64", "ARM64": "arm64"}
    if runner_arch not in arch_map:
        print(f"::error::Unsupported runner arch: {runner_arch}", file=sys.stderr)
        sys.exit(1)
    gcx_arch = arch_map[runner_arch]

    archive = f"gcx_{asset_version}_{gcx_os}_{gcx_arch}.{ext}"
    checksums = f"gcx_{asset_version}_checksums.txt"
    base = f"https://github.com/{REPO}/releases/download/{tag}"

    # --- 3. Download archive + checksums ------------------------------------
    work = os.path.join(os.environ["RUNNER_TEMP"], "setup-gcx")
    os.makedirs(work, exist_ok=True)

    archive_path = os.path.join(work, archive)
    checksums_path = os.path.join(work, checksums)

    print(f"==> Downloading {archive}")
    try:
        download(f"{base}/{archive}", archive_path)
    except urllib.error.URLError:
        print(f"::error::Failed to download {archive}", file=sys.stderr)
        sys.exit(1)
    try:
        download(f"{base}/{checksums}", checksums_path)
    except urllib.error.URLError:
        print(f"::error::Failed to download {checksums}", file=sys.stderr)
        sys.exit(1)

    # --- 4. Verify sha256 (fail hard on mismatch) ---------------------------
    print("==> Verifying checksum")
    # Match the exact filename field so metacharacters in the name aren't an issue.
    expected = None
    with open(checksums_path, encoding="utf-8") as f:
        for line in f:
            parts = line.split()
            if len(parts) == 2 and parts[1] == archive:
                expected = parts[0]
                break
    if not expected:
        print(f"::error::No checksum entry for {archive} in {checksums}", file=sys.stderr)
        sys.exit(1)

    sha = hashlib.sha256()
    with open(archive_path, "rb") as f:
        for chunk in iter(lambda: f.read(65536), b""):
            sha.update(chunk)
    actual = sha.hexdigest()

    if expected != actual:
        print(
            f"::error::Checksum mismatch for {archive}: expected {expected}, got {actual}",
            file=sys.stderr,
        )
        sys.exit(1)

    # --- 5. Extract ---------------------------------------------------------
    tool_dir = os.path.join(work, "bin")
    os.makedirs(tool_dir, exist_ok=True)
    print(f"==> Extracting {binary}")
    if ext == "zip":
        with zipfile.ZipFile(archive_path) as zf:
            zf.extract(binary, tool_dir)
    else:
        with tarfile.open(archive_path, "r:gz") as tf:
            tf.extract(binary, tool_dir)

    bin_path = os.path.join(tool_dir, binary)
    if not os.path.isfile(bin_path):
        print(f"::error::Expected binary {binary} not found in {archive}", file=sys.stderr)
        sys.exit(1)
    st = os.stat(bin_path)
    os.chmod(bin_path, st.st_mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)

    # --- 6. Export PATH + outputs -------------------------------------------
    with open(os.environ["GITHUB_PATH"], "a", encoding="utf-8") as f:
        f.write(tool_dir + "\n")
    with open(os.environ["GITHUB_OUTPUT"], "a", encoding="utf-8") as f:
        f.write(f"version={tag}\n")
        f.write(f"path={bin_path}\n")

    print(f"==> Installed gcx {tag} at {bin_path}")


if __name__ == "__main__":
    main()
