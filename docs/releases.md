# Releases

Commits follow Conventional Commits. [release-please](https://github.com/googleapis/release-please) opens a release PR (changelog, version bump). Merging it tags `vX.Y.Z`. Do not tag by hand.

## Releasing

Nothing is released by merging to `main`. `release-please.yml` runs on `workflow_dispatch` only:

1. `gh workflow run release-please.yml` — release-please opens or updates the release PR and dispatches `ci.yml` on that branch.
2. Merge the release PR to `main` after CI is green.
3. `gh workflow run release-please.yml` again — the `vX.Y.Z` tag and the GitHub release are created, and `release.yml` is dispatched for that tag.

Step 3 cannot be skipped. The tag is pushed with `GITHUB_TOKEN`, and a push with that token starts no workflow, which is why `release-please.yml` dispatches `release.yml` itself. `release.yml` can also be started by hand for a tag that already exists (`gh workflow run release.yml --ref vX.Y.Z`).

How `release-please` dispatches CI, and why `ci.yml` triggers on every branch push, is in [development.md](development.md#ci).

`release.yml` and `release-dry-run` both call [.github/actions/goreleaser](../.github/actions/goreleaser/action.yml). That action installs Cosign outside the checkout, refuses a dirty tree, then runs GoReleaser v2 ([.goreleaser.yaml](../.goreleaser.yaml)):

- `linux`, `amd64` and `arm64`, `CGO_ENABLED=0`, `-trimpath`, with version, commit and date stamped in
- multi-arch image `ghcr.io/blackdark/vc-belegapp` tagged `X.Y.Z`, `X.Y`, `X`, and `latest`
- Cosign keyless signatures on the image, by digest
- a buildx SBOM attestation and a GitHub build-provenance attestation, both on the image digest
- release notes come from release-please (`changelog.disable`, `release.mode: append`); the footer adds the image reference

**The image is the only artifact.** No binary tarballs, no `checksums.txt`, no Syft documents, no macOS build. An image is addressed by digest, so a checksum beside it would add nothing, and the two binaries inside it are the same ones CI builds; the SBOM and the provenance attestation describe the image itself. The previous profile published four tarballs and their signatures, and nothing but the image was ever deployed — recorded in [PROGRESS.md](../PROGRESS.md).

The release notes name `ghcr.io/blackdark/vc-belegapp:<version>` (no `v`). Git tag `v1.1.1` is the image tag `1.1.1`.

## Verify a release

The identity is the release workflow on a `v*` tag:

```bash
cosign verify \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity-regexp '^https://github.com/BlackDark/vc-belegapp/\.github/workflows/release\.yml@refs/tags/v' \
  ghcr.io/blackdark/vc-belegapp:1.1.1

gh attestation verify oci://ghcr.io/blackdark/vc-belegapp:1.1.1 --owner BlackDark
```

The SBOM attestation is a registry referrer beside the image, not a file in the release; it is what carries the package list that used to sit in a Syft document.
