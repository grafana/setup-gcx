# setup-gcx

A GitHub Action that installs the [`gcx`](https://github.com/grafana/gcx) CLI and adds it to
`PATH`, so your workflows can manage Grafana Cloud resources as code.

Follows the `actions/setup-go` / `grafana/setup-k6-action` pattern: it downloads the matching
[gcx release](https://github.com/grafana/gcx/releases) asset for the runner, verifies its
checksum, and puts the binary on `PATH`.

## Usage

```yaml
# Pin a version (recommended for reproducibility)
- uses: grafana/setup-gcx@v1
  with:
    version: v1.3.0
- run: gcx version

# Always install the latest release
- uses: grafana/setup-gcx@v1
  with:
    version: latest
- run: gcx version
```

The action needs read access to the `grafana/gcx` releases. On public repos the default
`github.token` is sufficient; make sure the job has at least:

```yaml
permissions:
  contents: read
```

## Inputs

| Input          | Default              | Description                                                        |
|----------------|----------------------|--------------------------------------------------------------------|
| `version`      | `latest`             | gcx version to install (e.g. `v1.3.0`) or `latest`.                |
| `github-token` | `${{ github.token }}`| Token for GitHub API calls (release lookup) to avoid rate limits.  |

## Outputs

| Output    | Description                                     |
|-----------|-------------------------------------------------|
| `version` | The resolved gcx version that was installed (e.g. `v1.3.0`). |
| `path`    | Absolute path to the installed `gcx` binary.    |

## Supported runners

All platforms published by gcx:

| OS      | `amd64` | `arm64` |
|---------|:-------:|:-------:|
| Linux   | ✅      | ✅      |
| macOS   | ✅      | ✅      |
| Windows | ✅      | ✅      |

## Matrix example

```yaml
jobs:
  test:
    strategy:
      matrix:
        os: [ubuntu-latest, macos-latest, windows-latest]
    runs-on: ${{ matrix.os }}
    permissions:
      contents: read
    steps:
      - uses: grafana/setup-gcx@v1
        with:
          version: latest
      - run: gcx version
```

## Version pinning

For reproducible builds, pin an exact gcx version (`version: v1.3.0`) and pin the action to the
floating major tag (`grafana/setup-gcx@v1`) or a full SHA. `latest` always resolves to the newest
published gcx release at run time.

## License

[Apache-2.0](./LICENSE)
