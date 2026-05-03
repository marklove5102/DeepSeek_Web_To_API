#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT_DIR"

source "${ROOT_DIR}/scripts/release-targets.sh"

OUT_DIR="${ROOT_DIR}/.tmp/cross-build"

github_annotation_escape() {
  tr '\n' ' ' | sed -e 's/%/%25/g' -e 's/\r/%0D/g'
}

emit_build_failure() {
  local label="$1" log_file="$2"
  local message

  message="$(tail -n 30 "$log_file" | github_annotation_escape)"
  if [[ -n "${GITHUB_ACTIONS:-}" ]]; then
    printf '::error file=tests/scripts/check-cross-build.sh,title=Cross-build failed (%s)::%s\n' "$label" "$message" >&2
  fi

  cat "$log_file" >&2
}

build_one() {
  local goos="$1" goarch="$2" goarm="$3" label="$4"
  local out log_file
  out="${OUT_DIR}/${label}/deepseek-web-to-api"
  if [[ "$goos" == "windows" ]]; then
    out="${out}.exe"
  fi

  echo "[cross-build] ${label}"
  mkdir -p "$(dirname "$out")"
  log_file="${OUT_DIR}/${label}/go-build.log"
  if [[ "$goarm" == "-" ]]; then
    if ! CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
      go build -buildvcs=false -trimpath -o "$out" ./cmd/DeepSeek_Web_To_API >"$log_file" 2>&1; then
      emit_build_failure "$label" "$log_file"
      return 1
    fi
  else
    if ! CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" GOARM="$goarm" \
      go build -buildvcs=false -trimpath -o "$out" ./cmd/DeepSeek_Web_To_API >"$log_file" 2>&1; then
      emit_build_failure "$label" "$log_file"
      return 1
    fi
  fi
}

if [[ "${1:-}" == "--build-one" ]]; then
  shift
  build_one "$@"
  exit 0
fi

jobs="${CROSS_BUILD_JOBS:-}"
if [[ -z "$jobs" ]]; then
  if command -v nproc >/dev/null 2>&1; then
    jobs="$(nproc)"
  elif command -v sysctl >/dev/null 2>&1; then
    jobs="$(sysctl -n hw.ncpu)"
  else
    jobs="2"
  fi
fi

rm -rf "$OUT_DIR"
mkdir -p "$OUT_DIR"

if [[ "$jobs" -le 1 ]]; then
  for target in "${DEEPSEEK_WEB_TO_API_RELEASE_TARGETS[@]}"; do
    read -r goos goarch goarm label <<< "$target"
    build_one "$goos" "$goarch" "$goarm" "$label"
  done
else
  printf '%s\n' "${DEEPSEEK_WEB_TO_API_RELEASE_TARGETS[@]}" \
    | xargs -L 1 -P "$jobs" bash "${ROOT_DIR}/tests/scripts/check-cross-build.sh" --build-one
fi
