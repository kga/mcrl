#!/bin/sh
# 配布物（アーカイブとコンテナイメージ）に入れる第三者のライセンス表示を
# THIRD_PARTY_LICENSES に書き出す（GoReleaser の before フック）。
#
# mcrl のバイナリには、依存モジュールと Go の標準ライブラリ・ランタイムが入る。
# MIT・BSD・Apache-2.0 はいずれも、バイナリで配布するときにもライセンス本文と
# 著作権表示（Apache-2.0 は NOTICE があればそれも）を添えることを求めるので、
# リリースする全 OS の依存を集めて 1 つのファイルにする。
set -eu

out=THIRD_PARTY_LICENSES

# OS ごとに依存が違う（Linux は D-Bus、Windows は資格情報マネージャーなど）ので、全 OS の和を取る
mods=$(
  for os in linux darwin windows; do
    GOOS=$os go list -deps -f '{{if not .Standard}}{{.Module.Path}}{{end}}' ./cmd/mcrl
  done | sort -u | grep -v '^github.com/kga/mcrl$'
)

section() {
  printf '================================================================================\n'
  printf '%s\n' "$1"
  printf '================================================================================\n\n'
}

{
  printf 'Third-party licenses\n\n'
  printf 'The mcrl binary includes the Go standard library and runtime and the modules\n'
  printf 'listed below. Their licenses and notices follow. mcrl itself is licensed under\n'
  printf 'the MIT License (see LICENSE).\n\n'

  section "Go (standard library and runtime) $(go env GOVERSION)"
  cat "$(go env GOROOT)/LICENSE"
  printf '\n'

  for m in $mods; do
    dir=$(go list -m -f '{{.Dir}}' "$m")
    ver=$(go list -m -f '{{.Version}}' "$m")
    found=
    for f in "$dir"/LICENSE* "$dir"/LICENCE* "$dir"/COPYING* "$dir"/NOTICE*; do
      [ -f "$f" ] || continue
      if [ -z "$found" ]; then
        section "$m $ver"
        found=1
      fi
      printf -- '--- %s ---\n\n' "$(basename "$f")"
      cat "$f"
      printf '\n'
    done
    if [ -z "$found" ]; then
      echo "no license file found for $m ($dir)" >&2
      exit 1
    fi
  done
} > "$out"
