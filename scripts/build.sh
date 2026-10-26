#!/bin/sh
# Puts the plugin binary at bin/herdr-scope. herdr-plugin.toml runs this as
# the install build step on Linux and macOS (scripts/build.cmd on Windows).
#
#   1. A release checkout (`--ref release` or `--ref vX.Y.Z-bin`) carries
#      prebuilt binaries as bin/herdr-scope-<os>-<arch>: copy this machine's.
#   2. Otherwise build from source with Go, which fetches the modules on
#      the first build.
#   3. Otherwise stop and say how to get one of the two.
set -eu

cd "$(dirname -- "$0")/.."

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
esac
prebuilt="bin/herdr-scope-$os-$arch"

if [ -f "$prebuilt" ]; then
	# Copy, then rename over the target: never rewrite a binary in place that
	# may be running.
	tmp="bin/.herdr-scope.$$"
	cp "$prebuilt" "$tmp"
	chmod 755 "$tmp"
	mv -f "$tmp" bin/herdr-scope
	echo "herdr-scope: installed the prebuilt $prebuilt"
	exit 0
fi

if command -v go >/dev/null 2>&1; then
	echo "herdr-scope: no prebuilt binary for $os-$arch, building from source with $(go env GOVERSION)"
	if ! go build -trimpath -o bin/herdr-scope ./cmd/herdr-scope; then
		echo "herdr-scope: go build failed; building needs Go 1.26 or newer (or install a release: herdr plugin install kriuchkov/herdr-scope --ref release)" >&2
		exit 1
	fi
	exit 0
fi

cat >&2 <<EOF
herdr-scope: cannot install: this checkout has no prebuilt binary for
$os-$arch and there is no Go toolchain on PATH to build one. Either install a
release, which ships prebuilt binaries for macOS, Linux and Windows on amd64
and arm64 (herdr plugin install kriuchkov/herdr-scope --ref release), or
install Go 1.26 or newer (https://go.dev/dl/) and run the install again.
EOF
exit 1
