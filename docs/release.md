# Release

Releases are tag driven. A pushed semver tag builds `memoryd` with GoReleaser on macOS arm64, creates a GitHub release, uploads checksummed archives, bundles the zvec native library, and publishes the `memoryd-cli` Homebrew cask to `tomnagengast/homebrew-tap`.

## Release Setup

The Homebrew tap lives at `tomnagengast/homebrew-tap`. Keep `HOMEBREW_TAP_GITHUB_TOKEN` set in the `tomnagengast/agent-memoryd` repository secrets. The token needs contents write access to that tap repository.

The release workflow uses the repository `GITHUB_TOKEN` for the GitHub release itself.

## Local Checks

Download native libraries and run the standard checks:

```sh
mise run zvec-libs
mise run fmt
mise run test
mise run build
./memoryd --version
```

Verify release packaging without publishing:

```sh
goreleaser check
PWD="$(pwd)" goreleaser release --snapshot --clean
```

The GoReleaser build uses cgo, downloads the macOS arm64 zvec library, bundles `libzvec_c_api.dylib`, and runs `scripts/clean-darwin-rpaths.sh` so the release binary loads the dylib from the archive or cask staging directory.

## Cut A Release

Update `CHANGELOG.md`, merge the release commit to `main`, then create and push the next semver tag:

```sh
git tag -a v0.1.0 -m "v0.1.0"
git push origin v0.1.0
```

The `release` workflow only publishes from tag refs matching `v*.*.*`.

## Post-Publish Verification

Install from Homebrew and verify the cask caveat path:

```sh
brew tap tomnagengast/tap
brew install --cask tomnagengast/tap/memoryd-cli
memoryd --version
memoryd init --fresh
memoryd status
```

If GitHub release publishing succeeds but the tap update fails, fix forward by rerunning the release workflow after correcting the tap token or by manually updating the generated cask with the published artifact URLs and checksums.
