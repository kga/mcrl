#!/bin/sh
# リリース用に bash / zsh / fish の補完スクリプトを completions/ に書き出す（GoReleaser の before フック）
set -eu
rm -rf completions
mkdir completions
for sh in bash zsh fish; do
  go run ./cmd/mcrl completion "$sh" > "completions/mcrl.$sh"
done
