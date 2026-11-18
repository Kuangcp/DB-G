#!/bin/sh
# Installs DB-G for the current user on Linux, without root:
#
#   ~/.config/db-g/bin/db-g
#   ~/.local/share/applications/db-g.desktop
#   ~/.local/share/icons/db-g.png
#   ~/.config/db-g/version
#
#   curl -fsSL https://github.com/__GITHUB_REPO__/releases/latest/download/install.sh | sh
#   sh install.sh [ARCHIVE]
#   sh install.sh --uninstall
#
# The release workflow replaces __VERSION__, __GITHUB_REPO__ and __PUBLIC_KEY__.
set -eu

app='db-g'
version='__VERSION__'
repo='__GITHUB_REPO__'
public_key='__PUBLIC_KEY__'
releases="https://github.com/$repo/releases/latest/download/"

config_home="${XDG_CONFIG_HOME:-$HOME/.config}"
data_home="${XDG_DATA_HOME:-$HOME/.local/share}"
app_dir="$config_home/$app"
bin_dir="$app_dir/bin"
bin_path="$bin_dir/$app"
version_file="$app_dir/version"
icon_dir="$data_home/icons"
icon_path="$icon_dir/$app.png"
desktop_dir="$data_home/applications"
desktop_file="$desktop_dir/$app.desktop"
download_url="${DB_G_DOWNLOAD_URL:-}"

usage() {
	cat <<USAGE
Usage: sh install.sh [ARCHIVE | --uninstall]

Installs $app for you in $bin_dir and registers its desktop entry.
ARCHIVE is db-g-<version>-linux-<arch>.tar.gz; without one, the latest
signed release is downloaded. --uninstall removes the installed files and
leaves settings and data in place.
USAGE
}

fail() {
	echo "install.sh: $*" >&2
	exit 1
}

fetch() {
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL "$1"
	else
		wget -qO- "$1"
	fi
}

field() {
	sed -n -e 's/^  "'"$1"'": "\([^"]*\)",\{0,1\}$/\1/p' \
		-e 's/^  "'"$1"'": \([0-9][0-9]*\),\{0,1\}$/\1/p' "$work/update.json" | head -n 1
}

verify_openssl() {
	printf '%s' "$public_key" | grep -Eq '^[A-Za-z0-9+/]{43}=$' || return 1
	printf '%s' "$2" | grep -Eq '^[A-Za-z0-9+/]{86}==$' || return 1
	openssl dgst -sha256 -binary <"$1" >"$work/digest" || return 1
	ed25519_verify "MCowBQYDK2VwAyEA$public_key" "$2" "$work/digest"
}

ed25519_verify() {
	printf '%s\n' '-----BEGIN PUBLIC KEY-----' "$1" '-----END PUBLIC KEY-----' >"$work/key.pem"
	printf '%s' "$2" | openssl base64 -d -A >"$work/signature" 2>/dev/null || return 1
	[ "$(wc -c <"$work/signature" | tr -d ' ')" = 64 ] || return 1
	openssl pkeyutl -verify -pubin -inkey "$work/key.pem" -rawin -in "$3" -sigfile "$work/signature" >/dev/null 2>&1
}

has_ed25519() {
	command -v openssl >/dev/null 2>&1 || return 1
	printf r >"$work/rfc8032"
	ed25519_verify MCowBQYDK2VwAyEAPUAXw+hDiVqStwqnTRt+vJyYLM8uxJaMwM1V8Sr0Zgw= \
		kqAJqfDUyrhyDoILX2QlQKKye1QWUD+Ps3YiI+vbadoIWsHkPhWZbkWPNhPQ8R2MOHsurrQwKu6wDSkWErsMAA== "$work/rfc8032"
}

build_number() {
	printf '%s\n' "${1%%-*}" | awk -F. '
		NF == 3 && $1 ~ /^[0-9]+$/ && $2 ~ /^[0-9]+$/ && $3 ~ /^[0-9]+$/ && $2 <= 99 && $3 <= 99 {
			printf "%d", $1*10000 + $2*100 + $3
			next
		}
		{ printf "0" }
	'
}

update_databases() {
	if command -v update-desktop-database >/dev/null 2>&1; then
		update-desktop-database "$desktop_dir" >/dev/null 2>&1 || true
	fi
	if command -v update-mime-database >/dev/null 2>&1; then
		update-mime-database "$data_home/mime" >/dev/null 2>&1 || true
	fi
}

write_desktop() {
	src="$1"
	dst="$2"
	if [ -f "$src" ]; then
		awk -v exec_line="Exec=\"$bin_path\"" -v icon_line="Icon=$icon_path" '
			/^Exec=/ { print exec_line; next }
			/^Icon=/ { print icon_line; next }
			{ print }
		' "$src" >"$dst"
	else
		cat >"$dst" <<DESKTOP
[Desktop Entry]
Type=Application
Version=1.0
Name=DB-G
Comment=SQLite / MySQL / PostgreSQL / ClickHouse database client
Exec="$bin_path"
Icon=$icon_path
Terminal=false
Categories=Development;Database;
StartupWMClass=db-g
DESKTOP
	fi
}

install_linux() {
	archive="${1:-}"
	[ "$(uname -s)" = Linux ] || fail "$app installs on Linux only"
	[ "$(id -u)" != 0 ] || fail "run this as the user to install $app for, not as root"
	case "$(uname -m)" in
	x86_64 | amd64) target=linux-amd64 ;;
	aarch64 | arm64) target=linux-arm64 ;;
	*) fail "$app is not made for $(uname -m)" ;;
	esac
	case "$bin_path$icon_path$desktop_file" in
	*'"'* | *'$'* | *'%'* | *'\'* | *'
'*) fail "the install path has characters a desktop entry cannot hold" ;;
	esac

	work="$(mktemp -d)"
	trap 'rm -rf "$work"' EXIT
	trap 'exit 1' HUP INT TERM
	latest=''

	if [ -n "$download_url" ]; then
		releases="${download_url%/}/"
	fi
	if [ -z "$archive" ] && [ -f "$0" ] && [ -f "$(dirname -- "$0")/$app-$version-$target.tar.gz" ]; then
		archive="$(dirname -- "$0")/$app-$version-$target.tar.gz"
	fi
	if [ -z "$archive" ]; then
		command -v curl >/dev/null 2>&1 || command -v wget >/dev/null 2>&1 ||
			fail "downloading $app needs curl or wget"
		has_ed25519 || fail "checking the release signature needs OpenSSL 3.0 or later"
		manifest="${releases}update-$target.json"
		fetch "$manifest" >"$work/update.json" || fail "could not download $manifest"
		latest="$(field version)"
		url="$(field url)"
		size="$(field size)"
		signature="$(field signature)"
		[ -n "$url" ] || fail "$manifest names no archive"
		[ -n "$size" ] && [ -n "$signature" ] || fail "$manifest names no signed archive"
		echo "Downloading DB-G $latest"
		archive="$work/$app.tar.gz"
		fetch "$url" >"$archive" || fail "could not download $url"
		[ "$(wc -c <"$archive" | tr -d ' ')" = "$size" ] ||
			fail "$url does not have the size of the manifest: not installing it"
		verify_openssl "$archive" "$signature" ||
			fail "$url is not signed with the key of DB-G: not installing it"
	fi
	[ -f "$archive" ] || fail "no archive at $archive"
	tar -tzf "$archive" >"$work/names" 2>/dev/null || fail "$archive is not a .tar.gz archive"
	if grep -Eq '^/|(^|/)\.\.(/|$)' "$work/names"; then
		fail "$archive holds files outside the app"
	fi

	mkdir -p "$work/app"
	tar -xzf "$archive" -C "$work/app"
	[ -f "$work/app/$app" ] && [ -x "$work/app/$app" ] || fail "$archive holds no executable $app"

	mkdir -p "$bin_dir"
	tmp="$bin_dir/.$app.new.$$"
	cp "$work/app/$app" "$tmp" || fail "could not install $bin_path"
	chmod 0755 "$tmp"
	mv -f "$tmp" "$bin_path" || fail "could not install $bin_path"

	if [ -f "$work/app/$app.png" ]; then
		mkdir -p "$icon_dir"
		tmp="$icon_dir/.$app.png.new.$$"
		cp "$work/app/$app.png" "$tmp" && chmod 0644 "$tmp" && mv -f "$tmp" "$icon_path" ||
			fail "could not install $icon_path"
	fi

	mkdir -p "$desktop_dir"
	tmp="$desktop_dir/.$app.desktop.new.$$"
	write_desktop "$work/app/$app.desktop" "$tmp" || fail "could not write the desktop entry"
	chmod 0644 "$tmp"
	mv -f "$tmp" "$desktop_file" || fail "could not install $desktop_file"

	installed="${latest:-$version}"
	build="$(build_number "$installed")"
	mkdir -p "$app_dir"
	tmp="$app_dir/.version.new.$$"
	printf '[db-g]\nbuild=%s\nversion=%s\n' "$build" "$installed" >"$tmp"
	chmod 0644 "$tmp"
	mv -f "$tmp" "$version_file" || fail "could not write $version_file"

	# An older mygo install.sh put the app in ~/.local/db-g.app and linked
	# ~/.local/bin/db-g; remove only entries that are clearly that app's.
	old_app="$HOME/.local/$app.app"
	if [ -x "$old_app/$app" ]; then
		rm -rf "$old_app"
	fi
	old_link="$HOME/.local/bin/$app"
	if [ -L "$old_link" ]; then
		case "$(readlink "$old_link")" in
		"$old_app/$app" | "$old_app"/*) rm -f "$old_link" ;;
		esac
	fi

	update_databases
	echo "Installed DB-G $installed: $bin_path"
	echo "Open it from the applications menu."
}

uninstall_linux() {
	installed=false
	if [ -e "$bin_path" ] || [ -d "$bin_dir" ]; then
		installed=true
	fi
	old_app="$HOME/.local/$app.app"
	if [ -x "$old_app/$app" ]; then
		installed=true
	fi
	[ "$installed" = true ] || fail "DB-G is not installed in $app_dir"

	rm -rf "$bin_dir"
	if [ -f "$desktop_file" ] &&
		{ grep -qF "$bin_path" "$desktop_file" || grep -qF "$old_app/" "$desktop_file"; }; then
		rm -f "$desktop_file"
	fi
	rm -f "$icon_path" "$version_file"
	rm -rf "$old_app"
	old_link="$HOME/.local/bin/$app"
	if [ -L "$old_link" ]; then
		case "$(readlink "$old_link")" in
		"$old_app/$app" | "$old_app"/*) rm -f "$old_link" ;;
		esac
	fi
	update_databases
	echo "Uninstalled DB-G. Settings and data are still in $app_dir."
}

case "${1:-}" in
-h | --help)
	usage
	exit 0
	;;
--uninstall)
	[ $# -eq 1 ] || fail "--uninstall takes no arguments"
	uninstall_linux
	exit 0
	;;
-*)
	usage >&2
	exit 2
	;;
esac
[ $# -le 1 ] || { usage >&2; exit 2; }
install_linux "$@"
