# Releases

Commits follow Conventional Commits. [release-please](https://github.com/googleapis/release-please) opens a release PR (changelog, version bump). Merging it tags `vX.Y.Z`. Do not tag by hand.

How `release-please` dispatches CI, and why `ci.yml` triggers on every branch push, is in [development.md](development.md#ci).

`release.yml` and `release-dry-run` both call [.github/actions/goreleaser](../.github/actions/goreleaser/action.yml). That action installs Cosign and Syft outside the checkout, refuses a dirty tree, then runs GoReleaser v2 ([.goreleaser.yaml](../.goreleaser.yaml)):

- `linux` and `darwin`, `amd64` and `arm64`, `CGO_ENABLED=0`, archives include `LICENSE`, `README.md`, and `deploy/`
- checksums (`checksums.txt`), SBOMs (syft)
- multi-arch image `ghcr.io/blackdark/vc-belegapp` tagged `X.Y.Z`, `X.Y`, `X`, and `latest`
- Cosign keyless signatures: `checksums.txt.sigstore.json`, and the image by digest
- GitHub artifact attestations on `checksums.txt` and the image digest
- release notes come from release-please (`changelog.disable`, `release.mode: append`); the footer adds the image reference

The release notes name `ghcr.io/blackdark/vc-belegapp:<version>` (no `v`). Git tag `v1.1.1` is the image tag `1.1.1`.

## Verify a release

The identity is the release workflow on a `v*` tag:

```bash
cosign verify \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity-regexp '^https://github.com/BlackDark/vc-belegapp/\.github/workflows/release\.yml@refs/tags/v' \
  ghcr.io/blackdark/vc-belegapp:1.1.1

cosign verify-blob \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity-regexp '^https://github.com/BlackDark/vc-belegapp/\.github/workflows/release\.yml@refs/tags/v' \
  --bundle checksums.txt.sigstore.json \
  checksums.txt

gh attestation verify oci://ghcr.io/blackdark/vc-belegapp:1.1.1 --owner BlackDark
```
