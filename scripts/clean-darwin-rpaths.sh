#!/usr/bin/env bash
set -euo pipefail

binary="${1:?usage: clean-darwin-rpaths.sh <binary> <keep-rpath>}"
keep="${2:?usage: clean-darwin-rpaths.sh <binary> <keep-rpath>}"

if ! command -v otool >/dev/null 2>&1 || ! command -v install_name_tool >/dev/null 2>&1; then
  echo "otool and install_name_tool are required to clean macOS rpaths" >&2
  exit 1
fi

while IFS= read -r rpath; do
  if [ "${rpath}" != "${keep}" ]; then
    install_name_tool -delete_rpath "${rpath}" "${binary}"
  fi
done < <(otool -l "${binary}" | awk '/cmd LC_RPATH/{getline; getline; print $2}')
