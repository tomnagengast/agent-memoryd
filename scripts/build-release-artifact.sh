#!/usr/bin/env bash
set -euo pipefail

version="${MEMORYD_VERSION:-${GITHUB_REF_NAME:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}}"
commit="${MEMORYD_COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || true)}"
date="${MEMORYD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
goos="${MEMORYD_GOOS:-$(go env GOOS)}"
goarch="${MEMORYD_GOARCH:-$(go env GOARCH)}"
zvec_version="${ZVEC_VERSION:-v0.5.0}"

case "${goos}/${goarch}" in
  darwin/arm64)
    zvec_platform="darwin_arm64"
    libext="dylib"
    rpath="@loader_path/../lib"
    ;;
  linux/amd64)
    zvec_platform="linux_amd64"
    libext="so"
    rpath="\$ORIGIN/../lib"
    ;;
  linux/arm64)
    zvec_platform="linux_arm64"
    libext="so"
    rpath="\$ORIGIN/../lib"
    ;;
  *)
    echo "unsupported release target: ${goos}/${goarch}" >&2
    exit 1
    ;;
esac

go run github.com/zvec-ai/zvec-go/cmd/download-libs@${zvec_version} -version "${zvec_version}" -dest ./lib

lib_path="lib/${zvec_platform}/libzvec_c_api.${libext}"
if [ ! -f "${lib_path}" ]; then
  echo "missing native zvec library: ${lib_path}" >&2
  exit 1
fi

module="github.com/tomnagengast/agent-memoryd/internal/version"
ldflags="-s -w -X ${module}.Version=${version} -X ${module}.Commit=${commit} -X ${module}.Date=${date}"
name="agent-memoryd_${version}_${goos}_${goarch}"
work="dist/${name}"

rm -rf "${work}"
mkdir -p "${work}/bin" "${work}/lib"

GOOS="${goos}" GOARCH="${goarch}" CGO_ENABLED=1 \
  CGO_CFLAGS="-I$(pwd)/lib/include" \
  CGO_LDFLAGS="-L$(pwd)/lib/${zvec_platform} -lzvec_c_api -Wl,-rpath,${rpath}" \
  go build -trimpath -ldflags "${ldflags}" -o "${work}/bin/memoryd" ./cmd/agent-memoryd

if [ "${goos}" = "darwin" ]; then
  while IFS= read -r existing_rpath; do
    if [ "${existing_rpath}" != "${rpath}" ]; then
      install_name_tool -delete_rpath "${existing_rpath}" "${work}/bin/memoryd"
    fi
  done < <(otool -l "${work}/bin/memoryd" | awk '/cmd LC_RPATH/{getline; getline; print $2}')
fi

cp "${lib_path}" "${work}/lib/"
tar -C "${work}" -czf "dist/${name}.tar.gz" bin lib
rm -rf "${work}"

echo "dist/${name}.tar.gz"
