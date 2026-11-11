#!/bin/sh
# Puts the plugin binary at bin/herdr-scope. herdr-plugin.toml runs this as
# the install build step on Linux and macOS (scripts/build.cmd on Windows).
#
#   1. A release checkout (`--ref release` or `--ref vX.Y.Z-bin`) carries
#      prebuilt binaries as bin/herdr-scope-<os>-<arch>: copy this machine's.
#   2. Otherwise build from source with Go, which fetches the modules on
#      the first build.
#   3. Otherwise, or when that build fails, download this version's release
#      archive and check it against the release's SHA256SUMS: how the plugin
#      catalog's plain `herdr plugin install` works without Go.
#   4. Otherwise stop and say how to get one of them.
set -eu

cd "$(dirname -- "$0")/.."

repo=kriuchkov/herdr-scope
version=$(sed -n 's/^version = "\(.*\)"$/\1/p' herdr-plugin.toml | head -n 1)
os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
esac
prebuilt="bin/herdr-scope-$os-$arch"

# place copies a binary to bin/herdr-scope, then renames it over the target:
# never rewrite a binary in place that may be running.
place() {
	mkdir -p bin || return 1
	tmp="bin/.herdr-scope.$$"
	cp "$1" "$tmp" && chmod 755 "$tmp" && mv -f "$tmp" bin/herdr-scope
}

# fetch downloads one release asset over HTTPS, three tries: curl's own
# --retry passes over a reset connection.
fetch() {
	for try in 1 2 3; do
		if command -v curl >/dev/null 2>&1; then
			curl -fsSL --proto '=https' --tlsv1.2 -o "$2" "$1" && return 0
		elif command -v wget >/dev/null 2>&1; then
			wget -q -O "$2" "$1" && return 0
		else
			echo "herdr-scope: neither curl nor wget is on PATH" >&2
			return 1
		fi
		sleep "$try"
	done
	return 1
}

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d ' ' -f 1
	else
		shasum -a 256 "$1" | cut -d ' ' -f 1
	fi
}

# download puts this version's release binary in place. The manifest here and
# the binary come from the same release: CI refuses a manifest that changed
# since the tag of its version.
download() {
	name="herdr-scope-$version-$os-$arch"
	url="https://github.com/$repo/releases/download/v$version"
	work=$(mktemp -d) || return 1
	trap 'rm -rf "$work"' EXIT
	echo "herdr-scope: downloading $name.tar.gz from the v$version release"
	fetch "$url/SHA256SUMS" "$work/SHA256SUMS" || return 1
	fetch "$url/$name.tar.gz" "$work/$name.tar.gz" || return 1
	want=$(awk -v f="$name.tar.gz" '$2 == f { print $1 }' "$work/SHA256SUMS")
	if [ -z "$want" ] || [ "$want" != "$(sha256 "$work/$name.tar.gz")" ]; then
		echo "herdr-scope: $name.tar.gz does not match the release's SHA256SUMS" >&2
		return 1
	fi
	tar -xzf "$work/$name.tar.gz" -C "$work" || return 1
	place "$work/$name/herdr-scope" || return 1
	if ! bin/herdr-scope version | grep -qF "herdr-scope $version "; then
		echo "herdr-scope: the downloaded binary is not version $version" >&2
		return 1
	fi
	echo "herdr-scope: installed the v$version release binary"
}

if [ -f "$prebuilt" ]; then
	place "$prebuilt"
	echo "herdr-scope: installed the prebuilt $prebuilt"
	exit 0
fi

if command -v go >/dev/null 2>&1; then
	# A broken toolchain prints no version; its go build says what is wrong.
	gover=$(go env GOVERSION 2>/dev/null) || gover=
	echo "herdr-scope: no prebuilt binary for $os-$arch, building from source with ${gover:-Go}"
	if go build -trimpath -o bin/herdr-scope ./cmd/herdr-scope; then
		exit 0
	fi
	echo "herdr-scope: go build failed (it needs Go 1.26 or newer); trying the release binary" >&2
fi

if download; then
	exit 0
fi

cat >&2 <<EOF
herdr-scope: cannot install: this checkout has no prebuilt binary for
$os-$arch, no Go toolchain built one, and the v$version release binary could
not be downloaded. Install a release, which ships prebuilt binaries for macOS,
Linux and Windows on amd64 and arm64 (herdr plugin install
kriuchkov/herdr-scope --ref release), or install Go 1.26 or newer
(https://go.dev/dl/) and run the install again.
EOF
exit 1
