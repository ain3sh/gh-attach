#!/usr/bin/env sh

set -eu

BINARY_NAME="gh-attach"
REPO="${GH_ATTACH_REPO:-ain3sh/gh-attach}"
DEFAULT_REF="${GH_ATTACH_REF:-}"
BIN_DIR="${GH_ATTACH_INSTALL_DIR:-$HOME/.local/bin}"
REF="$DEFAULT_REF"

usage() {
	cat <<EOF
Usage: ./install.sh [--bin-dir DIR] [--ref REF]

Install gh-attach from the current checkout or, when run remotely, from the latest GitHub release.

Options:
  --bin-dir DIR  Install into DIR instead of \$GH_ATTACH_INSTALL_DIR or ~/.local/bin
  --ref REF      Specific release tag or git ref to install instead of the latest release
  --help, -h     Show this help text

Environment:
  GH_ATTACH_INSTALL_DIR  Default install directory override
  GH_ATTACH_REPO         Source repository to download when not in a checkout (default: $REPO)
  GH_ATTACH_REF          Default release tag or git ref override
EOF
}

while [ "$#" -gt 0 ]; do
	case "$1" in
		--bin-dir)
			if [ "$#" -lt 2 ]; then
				echo "Error: --bin-dir requires a value" >&2
				exit 1
			fi
			BIN_DIR="$2"
			shift 2
			;;
		--ref)
			if [ "$#" -lt 2 ]; then
				echo "Error: --ref requires a value" >&2
				exit 1
			fi
			REF="$2"
			shift 2
			;;
		--help|-h)
			usage
			exit 0
			;;
		*)
			echo "Error: unknown argument $1" >&2
			usage >&2
			exit 1
			;;
	esac
done

if ! command -v go >/dev/null 2>&1; then
	echo "Error: Go is required to build gh-attach" >&2
	exit 1
fi

if command -v curl >/dev/null 2>&1; then
	DOWNLOAD_TOOL="curl"
elif command -v wget >/dev/null 2>&1; then
	DOWNLOAD_TOOL="wget"
else
	echo "Error: either curl or wget is required" >&2
	exit 1
fi

TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT INT TERM

download() {
	url="$1"
	output="$2"

	case "$DOWNLOAD_TOOL" in
		curl)
			curl -fsSL "$url" -o "$output"
			;;
		wget)
			wget -qO "$output" "$url"
			;;
	esac
}

resolve_latest_release_ref() {
	metadata="$TMP_DIR/latest-release.json"
	api_url="https://api.github.com/repos/$REPO/releases/latest"

	if ! download "$api_url" "$metadata"; then
		echo "Error: failed to fetch latest release metadata from $REPO" >&2
		exit 1
	fi

	ref=$(tr '\n' ' ' < "$metadata" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
	if [ -z "$ref" ]; then
		echo "Error: no published releases found for $REPO; pass --ref explicitly or install from a checkout" >&2
		exit 1
	fi

	printf '%s\n' "$ref"
}

download_source_archive() {
	ref="$1"
	output="$2"
	tag_url="https://github.com/$REPO/archive/refs/tags/$ref.tar.gz"
	branch_url="https://github.com/$REPO/archive/refs/heads/$ref.tar.gz"

	if download "$tag_url" "$output"; then
		return 0
	fi

	if download "$branch_url" "$output"; then
		return 0
	fi

	return 1
}

resolve_source_dir() {
	if [ -f "./go.mod" ] && [ -f "./main.go" ]; then
		printf '%s\n' "$(pwd)"
		return 0
	fi

	SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" 2>/dev/null && pwd || true)
	if [ -n "$SCRIPT_DIR" ] && [ -f "$SCRIPT_DIR/go.mod" ] && [ -f "$SCRIPT_DIR/main.go" ]; then
		printf '%s\n' "$SCRIPT_DIR"
		return 0
	fi

	if [ -z "$REF" ]; then
		REF=$(resolve_latest_release_ref)
	fi

	archive="$TMP_DIR/source.tar.gz"
	source_root="$TMP_DIR/source"

	mkdir -p "$source_root"
	echo "Downloading $REPO@$REF..." >&2
	if ! download_source_archive "$REF" "$archive"; then
		echo "Error: failed to download source archive for $REPO@$REF" >&2
		exit 1
	fi
	tar -xzf "$archive" -C "$source_root"

	first_dir=$(find "$source_root" -mindepth 1 -maxdepth 1 -type d | head -n 1)
	if [ -z "$first_dir" ] || [ ! -f "$first_dir/go.mod" ] || [ ! -f "$first_dir/main.go" ]; then
		echo "Error: downloaded source archive did not contain a buildable checkout" >&2
		exit 1
	fi

	printf '%s\n' "$first_dir"
}

SOURCE_DIR=$(resolve_source_dir)

mkdir -p "$BIN_DIR"

(
	cd "$SOURCE_DIR"
	go build -o "$TMP_DIR/$BINARY_NAME" .
)

install_path="$BIN_DIR/$BINARY_NAME"
cp "$TMP_DIR/$BINARY_NAME" "$install_path"
chmod +x "$install_path"

echo "Installed $BINARY_NAME to $install_path"

case ":$PATH:" in
	*":$BIN_DIR:"*) ;;
	*)
		echo "Add $BIN_DIR to your PATH to run '$BINARY_NAME' directly."
		;;
esac
