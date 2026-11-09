#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTALL_DIR="${HOME}/.local/bin"
BIN_NAME="wiimote-motionplus"

USER_ONLY=0
NO_DAEMON=0
SHOW_HELP=0
for arg in "$@"; do
    case "$arg" in
        --user-only) USER_ONLY=1 ;;
        --no-daemon) NO_DAEMON=1 ;;
        --help|-h) SHOW_HELP=1 ;;
        *) printf 'unknown argument: %s\n' "$arg" >&2; exit 2 ;;
    esac
done
if [ "$SHOW_HELP" = 1 ]; then
    printf 'Usage: %s [--user-only] [--no-daemon] [--help]\n' "$0"
    printf 'Default: configure system dependencies/permissions (sudo), install and enable/start the user daemon.\n'
    printf '  --user-only  Skip system setup; install binary/config and user daemon (Go required).\n'
    printf '  --no-daemon  Do not install, enable, start or stop any user service.\n'
    exit 0
fi
UDEV_RULE=/etc/udev/rules.d/99-wiimote-mouse-uinput.rules

log()  { printf '\033[1;32m==>\033[0m %s\n' "$1"; }
warn() { printf '\033[1;33m!!\033[0m %s\n' "$1"; }
die()  { printf '\033[1;31merror:\033[0m %s\n' "$1" >&2; exit 1; }

[ "$(uname -s)" = "Linux" ] || die "this only works on Linux"

if [ "$USER_ONLY" = 0 ]; then
# --- detect package manager -------------------------------------------------
PKG_INSTALL=""
if command -v pacman >/dev/null 2>&1; then
    PKG_INSTALL="sudo pacman -S --needed --noconfirm"
elif command -v apt >/dev/null 2>&1; then
    PKG_INSTALL="sudo apt install -y"
elif command -v dnf >/dev/null 2>&1; then
    PKG_INSTALL="sudo dnf install -y"
elif command -v zypper >/dev/null 2>&1; then
    PKG_INSTALL="sudo zypper install -y"
else
    warn "unknown package manager; install dependencies (go, bluez) yourself"
fi

install_pkg() {
    local name="$1"
    [ -n "$PKG_INSTALL" ] || return 1
    log "installing $name"
    $PKG_INSTALL "$name"
}

# --- go ----------------------------------------------------------------------
if ! command -v go >/dev/null 2>&1; then
    warn "go not found"
    case "$PKG_INSTALL" in
        *pacman*) install_pkg go || die "install go manually" ;;
        *apt*)    install_pkg golang-go || die "install go manually" ;;
        *dnf*)    install_pkg golang || die "install go manually" ;;
        *zypper*) install_pkg go || die "install go manually" ;;
        *) die "install go manually (https://go.dev/dl/)" ;;
    esac
fi
log "go: $(go version)"

# --- bluez / bluetoothctl -----------------------------------------------------
if ! command -v bluetoothctl >/dev/null 2>&1; then
    warn "bluetoothctl not found (bluez not installed)"
    case "$PKG_INSTALL" in
        *pacman*) install_pkg bluez; install_pkg bluez-utils ;;
        *apt*)    install_pkg bluez ;;
        *dnf*)    install_pkg bluez ;;
        *zypper*) install_pkg bluez ;;
        *) die "install bluez manually" ;;
    esac
fi
log "bluetoothctl: $(command -v bluetoothctl)"

if command -v systemctl >/dev/null 2>&1; then
    if ! systemctl is-active --quiet bluetooth; then
        log "starting bluetooth service"
        sudo systemctl enable --now bluetooth
    fi
else
    warn "no systemctl; start the bluetooth service yourself"
fi

# --- optional desktop notifications -------------------------------------------
if ! command -v notify-send >/dev/null 2>&1; then
    case "$PKG_INSTALL" in
        *pacman*|*dnf*) install_pkg libnotify || warn "notifications unavailable; install libnotify" ;;
        *apt*) install_pkg libnotify-bin || warn "notifications unavailable; install libnotify-bin" ;;
        *zypper*) install_pkg libnotify-tools || warn "notifications unavailable; install libnotify-tools" ;;
        *) warn "optional notify-send missing; battery warnings remain in the journal" ;;
    esac
fi

# --- allow HID connections from unbonded devices -------------------------------
# A Wii Remote doesn't support standard Bluetooth pairing (no SSP), so it never
# becomes "Paired"/"Bonded". BlueZ's input plugin defaults to
# ClassicBondedOnly=true, which silently refuses to bring up the HID channels
# for such a device - bluetoothctl still reports "Connection successful" (the
# ACL link connects fine), but no uhid device is ever created, so hid-wiimote
# never binds and no /dev/input nodes appear. This has to be set to false.
INPUT_CONF=/etc/bluetooth/input.conf
if [ -f "$INPUT_CONF" ] && grep -qE '^\s*ClassicBondedOnly\s*=\s*false' "$INPUT_CONF"; then
    :
else
    log "setting ClassicBondedOnly=false in $INPUT_CONF (required for Wii Remote HID to work)"
    if [ ! -f "$INPUT_CONF" ]; then
        printf '[General]\nClassicBondedOnly=false\n' | sudo tee "$INPUT_CONF" >/dev/null
    elif grep -qE '^\s*#?\s*ClassicBondedOnly\s*=' "$INPUT_CONF"; then
        sudo sed -i -E 's/^\s*#?\s*ClassicBondedOnly\s*=.*/ClassicBondedOnly=false/' "$INPUT_CONF"
    elif grep -qE '^\[General\]' "$INPUT_CONF"; then
        sudo sed -i '/^\[General\]/a ClassicBondedOnly=false' "$INPUT_CONF"
    else
        printf '\n[General]\nClassicBondedOnly=false\n' | sudo tee -a "$INPUT_CONF" >/dev/null
    fi
    if command -v systemctl >/dev/null 2>&1; then
        log "restarting bluetooth service to apply it"
        sudo systemctl restart bluetooth
    fi
fi

# --- hid-wiimote kernel module -------------------------------------------------
if ! modinfo hid-wiimote >/dev/null 2>&1; then
    warn "hid-wiimote module not found (fine if it's built into the kernel; if things don't work, check the HID_WIIMOTE kernel config)"
else
    log "hid-wiimote module present (auto-loads when a Wii Remote connects)"
fi

# --- uinput module -------------------------------------------------------------
# Consume the full module list: grep -q can SIGPIPE lsmod under pipefail.
if ! lsmod | grep '^uinput' >/dev/null; then
    log "loading uinput module"
    sudo modprobe uinput || warn "modprobe uinput failed; fine if it's built into the kernel"
fi
if [ ! -f /etc/modules-load.d/uinput.conf ]; then
    log "adding uinput to modules loaded at boot"
    echo uinput | sudo tee /etc/modules-load.d/uinput.conf >/dev/null
fi

# --- /dev/uinput permissions (so root isn't needed to run it) -----------------
if [ ! -f "$UDEV_RULE" ]; then
    log "adding udev rule: opening up /dev/uinput to the input group"
    echo 'KERNEL=="uinput", GROUP="input", MODE="0660"' | sudo tee "$UDEV_RULE" >/dev/null
    sudo udevadm control --reload-rules
    sudo udevadm trigger --name-match=uinput
fi

# --- add the user to the input group ------------------------------------------
if ! id -nG "$USER" | grep -qw input; then
    log "adding $USER to the input group (requires re-login to take effect)"
    sudo usermod -aG input "$USER"
    NEED_RELOGIN=1
fi

fi # system setup (never run in --user-only mode)

command -v go >/dev/null 2>&1 || die "Go is required for --user-only; install Go first"

# --- build & install -----------------------------------------------------------
# Build outside the checkout. Stage beside the destination and rename so an
# already-running executable can keep using its old inode (no ETXTBSY).
BUILD_DIR=""
STAGED_BIN=""
STAGED_CONFIG=""
STAGED_UNIT=""
cleanup() {
    [ -z "$BUILD_DIR" ] || rm -rf -- "$BUILD_DIR"
    [ -z "$STAGED_BIN" ] || rm -f -- "$STAGED_BIN"
    [ -z "$STAGED_CONFIG" ] || rm -f -- "$STAGED_CONFIG"
    [ -z "$STAGED_UNIT" ] || rm -f -- "$STAGED_UNIT"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
BUILD_DIR="$(mktemp -d "${TMPDIR:-/tmp}/wiimote-motionplus.XXXXXXXX")"
log "building"
(cd "$SCRIPT_DIR" && go build -o "$BUILD_DIR/$BIN_NAME" .)

mkdir -p "$INSTALL_DIR"
STAGED_BIN="$(mktemp "$INSTALL_DIR/.$BIN_NAME.XXXXXXXX")"
install -m 0755 "$BUILD_DIR/$BIN_NAME" "$STAGED_BIN"
mv -f -- "$STAGED_BIN" "$INSTALL_DIR/$BIN_NAME"
STAGED_BIN=""
log "installed: $INSTALL_DIR/$BIN_NAME"

CONFIG_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/wiimote-motionplus"
CONFIG_FILE="$CONFIG_DIR/config.json"
mkdir -p "$CONFIG_DIR"
if [ -e "$CONFIG_FILE" ] || [ -L "$CONFIG_FILE" ]; then
    log "preserved existing config: $CONFIG_FILE"
else
    # Publish a complete file without replacing ANY existing directory entry,
    # including dangling symlinks and a concurrent installer's new config.
    STAGED_CONFIG="$(mktemp "$CONFIG_DIR/.config.json.XXXXXXXX")"
    cat "$SCRIPT_DIR/config.default.json" > "$STAGED_CONFIG"
    if ln -T -- "$STAGED_CONFIG" "$CONFIG_FILE" 2>/dev/null; then
        log "created config: $CONFIG_FILE"
    elif [ -e "$CONFIG_FILE" ] || [ -L "$CONFIG_FILE" ]; then
        log "preserved existing config: $CONFIG_FILE"
    else
        die "could not create config: $CONFIG_FILE"
    fi
    rm -f -- "$STAGED_CONFIG"
    STAGED_CONFIG=""
fi

case ":$PATH:" in
    *":$INSTALL_DIR:"*) ;;
    *) warn "$INSTALL_DIR is not in PATH; add it to your shell config" ;;
esac

if [ "${NEED_RELOGIN:-0}" = "1" ]; then
    warn "log out and back in for the input group membership to take effect"
fi

# --- persistent user service -------------------------------------------------
if [ "$NO_DAEMON" = 0 ]; then
    UNIT_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
    UNIT_FILE="$UNIT_DIR/wiimote-motionplus.service"
    # Unit values are quoted systemd syntax, NOT shell commands. Preserve spaces,
    # backslashes and quotes; literal percent signs must not become specifiers.
    unit_escape() {
        local value="$1"
        case "$value" in *$'\n'*|*$'\r'*) die "newlines are not supported in service paths" ;; esac
        value="${value//\\/\\\\}"
        value="${value//\"/\\\"}"
        value="${value//%/%%}"
        printf '%s' "$value"
    }
    CONFIG_ROOT="$(unit_escape "${XDG_CONFIG_HOME:-$HOME/.config}")"
    # EnvironmentFile takes a single raw absolute filename (not a quoted
    # Environment= assignment); spaces/quotes are literal, % is a specifier.
    ENV_PATH="${CONFIG_DIR//%/%%}/daemon.env"
    mkdir -p "$UNIT_DIR"
    STAGED_UNIT="$(mktemp "$UNIT_DIR/.wiimote-motionplus.service.XXXXXXXX")"
    while IFS= read -r line || [ -n "$line" ]; do
        case "$line" in
            'Environment="XDG_CONFIG_HOME='*) printf 'Environment="XDG_CONFIG_HOME=%s"\n' "$CONFIG_ROOT" ;;
            EnvironmentFile=*) printf 'EnvironmentFile=-%s\n' "$ENV_PATH" ;;
            *) printf '%s\n' "$line" ;;
        esac
    done < "$SCRIPT_DIR/wiimote-motionplus.service" > "$STAGED_UNIT"
    chmod 0644 "$STAGED_UNIT"
    mv -fT -- "$STAGED_UNIT" "$UNIT_FILE"
    STAGED_UNIT=""
    log "installed user unit: $UNIT_FILE (daemon.env preserved)"
    service_failure() {
        warn "User service was NOT confirmed active. A logged-in systemd user session/bus and input permissions are required."
        printf 'After fixing the user session, run:\n  systemctl --user daemon-reload\n  systemctl --user enable --now wiimote-motionplus.service\n  systemctl --user status wiimote-motionplus.service\n' >&2
        die "user service setup failed; use --no-daemon to install without service changes"
    }
    command -v systemctl >/dev/null 2>&1 || service_failure
    systemctl --user daemon-reload || service_failure
    systemctl --user enable --now wiimote-motionplus.service || service_failure
    # enable --now leaves an existing daemon on its old executable inode.
    systemctl --user restart wiimote-motionplus.service || service_failure
    systemctl --user is-active --quiet wiimote-motionplus.service || service_failure
    log "user service enabled and active: wiimote-motionplus.service"
else
    log "daemon setup skipped (--no-daemon); existing services are unchanged"
fi

if [ "$NO_DAEMON" = 0 ]; then
    log "Press 1+2 (or the red SYNC button) on your Wii Remote until the LEDs blink."
    log "The daemon automatically discovers/trusts exact-name Nintendo Wii remotes and connects them. Bluetooth cannot wake a powered-off remote."
    log "Keep the remote stationary for one second after connection for gyro calibration."
    log "Active service does not yet mean mouse ready: inspect $BIN_NAME --status and journalctl --user -u wiimote-motionplus.service -n 30 --no-pager"
    log "Follow connection/calibration logs: journalctl --user -u wiimote-motionplus.service -f"
else
    log "Run $BIN_NAME --daemon for automatic Bluetooth connection, or $BIN_NAME --connect for one-shot connection. Press 1+2 first."
fi
