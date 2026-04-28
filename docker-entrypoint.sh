#!/bin/sh
set -eu

APP_USER="${APP_USER:-appuser}"
APP_DATA_DIR="${APP_DATA_DIR:-/app/data}"
DEFAULT_APP_UID=10001
DEFAULT_APP_GID=10001

detect_owner_id() {
	path="$1"
	format="$2"
	if [ -e "$path" ]; then
		stat -c "$format" "$path" 2>/dev/null || true
	fi
}

resolve_id() {
	configured="$1"
	detected="$2"
	default_value="$3"

	if [ -n "$configured" ]; then
		echo "$configured"
		return
	fi
	if [ -n "$detected" ] && [ "$detected" != "0" ]; then
		echo "$detected"
		return
	fi
	echo "$default_value"
}

assert_writable() {
	path="$1"
	if ! gosu "$APP_USER" sh -c "test -w \"$1\"" -- "$path"; then
		cat >&2 <<EOF
error: $path is not writable by uid:gid $APP_UID:$APP_GID.
Fix the mounted data directory on the host, for example:
  sudo chown -R $APP_UID:$APP_GID <host-data-dir>
or set PUID/PGID to match the host directory owner.
EOF
		exit 1
	fi
}

if [ "$(id -u)" = "0" ]; then
	mkdir -p "$APP_DATA_DIR"

	APP_UID="$(resolve_id "${PUID:-}" "$(detect_owner_id "$APP_DATA_DIR" "%u")" "$DEFAULT_APP_UID")"
	APP_GID="$(resolve_id "${PGID:-}" "$(detect_owner_id "$APP_DATA_DIR" "%g")" "$DEFAULT_APP_GID")"

	if [ "$(id -g "$APP_USER")" != "$APP_GID" ]; then
		groupmod -o -g "$APP_GID" "$APP_USER"
	fi
	if [ "$(id -u "$APP_USER")" != "$APP_UID" ]; then
		usermod -o -u "$APP_UID" "$APP_USER"
	fi

	assert_writable "$APP_DATA_DIR"

	exec gosu "$APP_USER" ./image-bed "$@"
fi

exec ./image-bed "$@"
