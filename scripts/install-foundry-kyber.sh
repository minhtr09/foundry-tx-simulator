#!/usr/bin/env bash
set -euo pipefail

foundry_tag="${FOUNDRY_KYBER_TAG:-nightly-04bdb43d9435cecec89abc17b3dcb618086c0d99}"
base_url="https://github.com/thepluck/foundry/releases/download/${foundry_tag}"
bin_dir="${FOUNDRY_BIN_DIR:-${HOME}/.foundry/bin}"

case "$(uname -s):$(uname -m)" in
  Linux:x86_64 | Linux:amd64)
    asset="foundry_nightly_linux_amd64.tar.gz"
    ;;
  Darwin:arm64)
    asset="foundry_nightly_darwin_arm64.tar.gz"
    ;;
  *)
    echo "unsupported platform: $(uname -s) $(uname -m)" >&2
    exit 1
    ;;
esac

tmp_dir="$(mktemp -d)"
cleanup() {
  rm -rf "${tmp_dir}"
}
trap cleanup EXIT

mkdir -p "${bin_dir}"
curl -fsSL "${base_url}/${asset}" -o "${tmp_dir}/${asset}"
curl -fsSL "${base_url}/${asset%.tar.gz}.sha256" -o "${tmp_dir}/${asset}.sha256"

expected_sha="$(awk '{print $1}' "${tmp_dir}/${asset}.sha256")"
if command -v sha256sum >/dev/null 2>&1; then
  actual_sha="$(sha256sum "${tmp_dir}/${asset}" | awk '{print $1}')"
else
  actual_sha="$(shasum -a 256 "${tmp_dir}/${asset}" | awk '{print $1}')"
fi
if [ "${actual_sha}" != "${expected_sha}" ]; then
  echo "${asset}: checksum mismatch" >&2
  exit 1
fi
echo "${asset}: OK"
tar -xzf "${tmp_dir}/${asset}" -C "${bin_dir}"

"${bin_dir}/forge-kyber" --version
