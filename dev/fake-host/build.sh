#!/bin/sh
# mcrl fake host のイメージをビルドする。API キーは不要・使用しない。
set -eu

here=$(cd "$(dirname "$0")" && pwd)
image=${MCRL_FAKE_HOST_IMAGE:-mcrl-fake-host:latest}

container build --tag "$image" "$here"
echo "built: $image"
