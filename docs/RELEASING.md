# Releasing kubeoptimizer

Releases are automated with [GoReleaser](https://goreleaser.com) via
`.github/workflows/release.yml`. Pushing a `v*` tag builds static
binaries (linux/darwin/windows, amd64/arm64), attaches them with
checksums to a GitHub Release, updates the Homebrew tap, and produces
a krew manifest.

## One-time setup

1. **Homebrew tap repo**: create `DPS0340/homebrew-tap` (public).
   GoReleaser writes `Casks/kubeoptimizer.rb` into it on each release
   (GoReleaser v2.10+ deprecated formulas for pre-built binaries in
   favor of casks).
2. **Tap token**: create a fine-grained PAT with `contents: write` on
   `DPS0340/homebrew-tap`, and add it to this repo's Actions secrets
   as `TAP_GITHUB_TOKEN`.

## Cutting a release

```sh
git tag -a v0.1.0 -m "Release v0.1.0"
git push origin v0.1.0
```

Then verify:

- GitHub Release has 6 archives + `checksums.txt`.
- `brew install DPS0340/tap/kubeoptimizer && kubeoptimizer version`.

## krew

GoReleaser renders `dist/krew/kubeoptimizer.yaml` (also uploaded as a
workflow artifact) with the correct per-platform sha256 sums. Upload
is disabled (`skip_upload: true`) because krew distribution goes
through the central [krew-index](https://github.com/kubernetes-sigs/krew-index),
not our own repo.

Local install test before submitting:

```sh
kubectl krew install --manifest=dist/krew/kubeoptimizer.yaml
kubectl kubeoptimizer version
```

First-time submission (after the GitHub Release is live):

1. Fork `kubernetes-sigs/krew-index`.
2. Copy `dist/krew/kubeoptimizer.yaml` to `plugins/kubeoptimizer.yaml`.
3. Open a PR — see the
   [new-plugin checklist](https://krew.sigs.k8s.io/docs/developer-guide/release/new-plugin/).

Subsequent version bumps can be automated with
[krew-release-bot](https://github.com/rajatjindal/krew-release-bot)
once the plugin is accepted.
