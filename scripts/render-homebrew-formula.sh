#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 3 ]; then
  echo "usage: $0 <version-tag> <checksums-file> <output-formula>" >&2
  exit 2
fi

version="$1"
checksums="$2"
output="$3"
template="packaging/homebrew/memoryd.rb.tpl"

if [ ! -f "${checksums}" ]; then
  echo "checksums file not found: ${checksums}" >&2
  exit 1
fi
if [ ! -f "${template}" ]; then
  echo "formula template not found: ${template}" >&2
  exit 1
fi

checksum_for() {
  local asset="$1"
  local checksum
  checksum="$(awk -v asset="${asset}" '$2 == asset { print $1 }' "${checksums}")"
  if [ -z "${checksum}" ]; then
    echo "missing checksum for ${asset}" >&2
    exit 1
  fi
  printf '%s' "${checksum}"
}

darwin_arm64_asset="agent-memoryd_${version}_darwin_arm64.tar.gz"
linux_amd64_asset="agent-memoryd_${version}_linux_amd64.tar.gz"
linux_arm64_asset="agent-memoryd_${version}_linux_arm64.tar.gz"

mkdir -p "$(dirname "${output}")"
VERSION="${version}" \
VERSION_NO_V="${version#v}" \
DARWIN_ARM64_SHA256="$(checksum_for "${darwin_arm64_asset}")" \
LINUX_AMD64_SHA256="$(checksum_for "${linux_amd64_asset}")" \
LINUX_ARM64_SHA256="$(checksum_for "${linux_arm64_asset}")" \
perl -0pe '
  s/\{\{VERSION\}\}/$ENV{VERSION}/g;
  s/\{\{VERSION_NO_V\}\}/$ENV{VERSION_NO_V}/g;
  s/\{\{DARWIN_ARM64_SHA256\}\}/$ENV{DARWIN_ARM64_SHA256}/g;
  s/\{\{LINUX_AMD64_SHA256\}\}/$ENV{LINUX_AMD64_SHA256}/g;
  s/\{\{LINUX_ARM64_SHA256\}\}/$ENV{LINUX_ARM64_SHA256}/g;
' "${template}" > "${output}"

echo "${output}"
