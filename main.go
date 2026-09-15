// Command setup-gcx resolves, downloads, verifies, and installs the gcx CLI.
// It is invoked by action.yml as a composite-action step and is the Go
// equivalent of the former install.sh.
//
// Expects env:
//
//	INPUT_VERSION  gcx version to install ("latest" or e.g. "v1.3.0")
//	GH_TOKEN       token for GitHub API calls (release lookup)
//	RUNNER_OS      Linux | macOS | Windows      (provided by the runner)
//	RUNNER_ARCH    X64 | ARM64                  (provided by the runner)
//	RUNNER_TEMP    scratch dir                  (provided by the runner)
//	GITHUB_PATH    file to append PATH entries  (provided by the runner)
//	GITHUB_OUTPUT  file to write step outputs   (provided by the runner)
package main

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	repo   = "grafana/gcx"
	apiURL = "https://api.github.com/repos/" + repo
)

func log(format string, args ...any) {
	fmt.Printf("==> "+format+"\n", args...)
}

// fail prints a GitHub Actions error annotation and exits non-zero.
func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "::error::"+format+"\n", args...)
	os.Exit(1)
}

func main() {
	if err := run(); err != nil {
		fail("%s", err)
	}
}

func run() error {
	// --- 1. Resolve version -------------------------------------------------
	// Normalize to tag form (v1.3.0) and asset form (1.3.0 — GoReleaser strips the v).
	versionInput := getenvOr("INPUT_VERSION", "latest")
	var tag string
	if versionInput == "latest" {
		log("Resolving latest gcx release")
		resolved, err := latestTag(os.Getenv("GH_TOKEN"))
		if err != nil {
			return fmt.Errorf("could not resolve the latest gcx release: %w", err)
		}
		tag = resolved
	} else {
		tag = versionInput
	}

	// tag keeps the leading v; asset filenames drop it.
	tag = "v" + strings.TrimPrefix(tag, "v")
	assetVersion := strings.TrimPrefix(tag, "v")
	log("Installing gcx %s", tag)

	// --- 2. Map runner OS/arch --> gcx asset naming -------------------------
	var gcxOS, ext, bin string
	switch os.Getenv("RUNNER_OS") {
	case "Linux":
		gcxOS, ext, bin = "linux", "tar.gz", "gcx"
	case "macOS":
		gcxOS, ext, bin = "darwin", "tar.gz", "gcx"
	case "Windows":
		gcxOS, ext, bin = "windows", "zip", "gcx.exe"
	default:
		return fmt.Errorf("unsupported runner OS: %s", os.Getenv("RUNNER_OS"))
	}

	var gcxArch string
	switch os.Getenv("RUNNER_ARCH") {
	case "X64":
		gcxArch = "amd64"
	case "ARM64":
		gcxArch = "arm64"
	default:
		return fmt.Errorf("unsupported runner arch: %s", os.Getenv("RUNNER_ARCH"))
	}

	archive := fmt.Sprintf("gcx_%s_%s_%s.%s", assetVersion, gcxOS, gcxArch, ext)
	checksums := fmt.Sprintf("gcx_%s_checksums.txt", assetVersion)
	base := fmt.Sprintf("https://github.com/%s/releases/download/%s", repo, tag)

	// --- 3. Download archive + checksums ------------------------------------
	work := filepath.Join(os.Getenv("RUNNER_TEMP"), "setup-gcx")
	if err := os.MkdirAll(work, 0o755); err != nil {
		return fmt.Errorf("could not create work dir: %w", err)
	}

	archivePath := filepath.Join(work, archive)
	checksumsPath := filepath.Join(work, checksums)

	log("Downloading %s", archive)
	if err := download(base+"/"+archive, archivePath); err != nil {
		return fmt.Errorf("failed to download %s: %w", archive, err)
	}
	if err := download(base+"/"+checksums, checksumsPath); err != nil {
		return fmt.Errorf("failed to download %s: %w", checksums, err)
	}

	// --- 4. Verify sha256 (fail hard on mismatch) ---------------------------
	log("Verifying checksum")
	expected, err := lookupChecksum(checksumsPath, archive)
	if err != nil {
		return err
	}
	actual, err := sha256File(archivePath)
	if err != nil {
		return fmt.Errorf("could not hash %s: %w", archive, err)
	}
	if expected != actual {
		return fmt.Errorf("checksum mismatch for %s: expected %s, got %s", archive, expected, actual)
	}

	// --- 5. Extract ---------------------------------------------------------
	toolDir := filepath.Join(work, "bin")
	if err := os.MkdirAll(toolDir, 0o755); err != nil {
		return fmt.Errorf("could not create tool dir: %w", err)
	}
	binPath := filepath.Join(toolDir, bin)
	log("Extracting %s", bin)
	if ext == "zip" {
		err = extractFromZip(archivePath, bin, binPath)
	} else {
		err = extractFromTarGz(archivePath, bin, binPath)
	}
	if err != nil {
		return fmt.Errorf("failed to extract %s from %s: %w", bin, archive, err)
	}
	if err := os.Chmod(binPath, 0o755); err != nil {
		return fmt.Errorf("could not mark %s executable: %w", binPath, err)
	}

	// --- 6. Export PATH + outputs -------------------------------------------
	if err := appendLine(os.Getenv("GITHUB_PATH"), toolDir); err != nil {
		return fmt.Errorf("could not update PATH: %w", err)
	}
	if err := appendLine(os.Getenv("GITHUB_OUTPUT"),
		"version="+tag, "path="+binPath); err != nil {
		return fmt.Errorf("could not write outputs: %w", err)
	}

	log("Installed gcx %s at %s", tag, binPath)
	return nil
}

func getenvOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// latestTag queries the GitHub API for the latest gcx release and returns its
// tag_name. The token is optional and only used to avoid rate limits.
func latestTag(token string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, apiURL+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned %s", resp.Status)
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", err
	}
	if release.TagName == "" {
		return "", errors.New("release response had no tag_name")
	}
	return release.TagName, nil
}

// download GETs url and writes the response body to dst.
func download(url, dst string) error {
	resp, err := http.Get(url) //nolint:gosec // url is built from a fixed repo + validated release tag
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s returned %s", url, resp.Status)
	}

	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		return err
	}
	return f.Close()
}

// lookupChecksum returns the expected sha256 for name from a GoReleaser
// checksums.txt (lines of "<hash>  <filename>"). The filename is matched
// exactly so dots in the name aren't treated as a pattern.
func lookupChecksum(path, name string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && fields[1] == name {
			return fields[0], nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("no checksum entry for %s in %s", name, filepath.Base(path))
}

// sha256File returns the lowercase hex sha256 of the file at path.
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// extractFromTarGz writes the entry named bin from a .tar.gz archive to dst.
func extractFromTarGz(archivePath, bin, dst string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("binary %s not found in archive", bin)
		}
		if err != nil {
			return err
		}
		if filepath.Base(hdr.Name) == bin {
			return writeFile(dst, tr)
		}
	}
}

// extractFromZip writes the entry named bin from a .zip archive to dst.
func extractFromZip(archivePath, bin, dst string) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, file := range r.File {
		if filepath.Base(file.Name) != bin {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return err
		}
		defer rc.Close()
		return writeFile(dst, rc)
	}
	return fmt.Errorf("binary %s not found in archive", bin)
}

// writeFile creates dst and copies src into it.
func writeFile(dst string, src io.Reader) error {
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, src); err != nil { //nolint:gosec // archive is checksum-verified before extraction
		return err
	}
	return out.Close()
}

// appendLine appends each line (with a trailing newline) to the file at path,
// used for the runner's GITHUB_PATH and GITHUB_OUTPUT files.
func appendLine(path string, lines ...string) error {
	if path == "" {
		return errors.New("target file path is empty")
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, line := range lines {
		if _, err := fmt.Fprintln(f, line); err != nil {
			return err
		}
	}
	return f.Close()
}
