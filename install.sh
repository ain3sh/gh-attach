#!/usr/bin/env sh

set -eu

BINARY_NAME="gh-attach"
BIN_DIR="${GH_ATTACH_INSTALL_DIR:-$HOME/.local/bin}"

usage() {
	cat <<EOF
Usage: ./install.sh [--bin-dir DIR]

Build and install gh-attach from the current checkout.

Options:
  --bin-dir DIR  Install into DIR instead of \$GH_ATTACH_INSTALL_DIR or ~/.local/bin
  --help, -h     Show this help text
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

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

if [ ! -f "$SCRIPT_DIR/go.mod" ] || [ ! -f "$SCRIPT_DIR/main.go" ]; then
	echo "Error: install.sh must be run from a gh-attach source checkout" >&2
	exit 1
fi

mkdir -p "$BIN_DIR"

TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT INT TERM

(
	cd "$SCRIPT_DIR"
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
