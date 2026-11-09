# wiimote-motionplus

A Linux Wii Remote air mouse with **MotionPlus gyroscope support** and the
original accelerometer/tilt mode. Reads evdev nodes created by `hid-wiimote`
and emits a virtual mouse through `/dev/uinput`. Go standard library only.

## Provenance

This is a separate repository derived from **kuroiko's wiimote-mouse**
(`kuroiko0429/wiimote` on GitHub), starting at commit
`12e1609b5c43d4d2997776839a14fd2f1600e14a`. The original MIT license and
copyright attribution are preserved in `LICENSE`. The original repository is
not required at runtime and is not modified by this project.

## Requirements and build

- 64-bit Linux evdev ABI (24-byte `input_event`), kernel `hid-wiimote` and `uinput`
- Wii Remote Plus, or an original Wii Remote with a MotionPlus attachment, for gyro mode
- An original Wii Remote without MotionPlus works in accelerometer mode
- Permission to read the selected `/dev/input/event*` nodes and write `/dev/uinput`
- BlueZ / `bluetoothctl` for automatic daemon connection and one-shot `--connect`
- Optional `notify-send` and a desktop notification service for low-battery popups
- Go 1.21+ to build

```sh
go test -race ./...
go vet ./...
go build -o wiimote-mouse .
./wiimote-mouse --help
```

## Install

```sh
./install.sh                  # system setup (may use sudo), install/start user daemon
# Or, if Bluetooth/uinput/input permissions are already configured:
./install.sh --user-only      # binary/config + user daemon; no privileged system setup
# Opt out of ALL user-service installation/management (also useful without a user bus):
./install.sh --user-only --no-daemon
wiimote-motionplus --help
```

Both modes build with Go and install **`~/.local/bin/wiimote-motionplus`**, leaving
an existing `wiimote-mouse` installation alone. Add `~/.local/bin` to your `PATH`
if necessary. `--user-only` requires Go to be installed already. The default mode
retains the system setup: dependencies, Bluetooth configuration (including
`ClassicBondedOnly=false`), service start/restart as needed, uinput/udev rules,
and input-group membership. Review `install.sh` before running full setup;
input-group membership grants broad access to input devices and requires logout/login.
`ClassicBondedOnly=false` relaxes the host-wide Bluetooth HID bonding restriction,
not just this application's access: unbonded HID devices can be accepted. Use it
only if that compatibility/security tradeoff is appropriate for your system.
Unless `--no-daemon` is supplied, **both modes install, enable, and start**
`wiimote-motionplus.service` for the current user's systemd session. A missing user
bus or a failed reload/start is an installation error, with manual recovery commands;
the installer never claims that an unverified service is active. `--no-daemon` does
not install/update/enable/start/stop a user unit, and leaves any existing service alone.
It still installs the binary and preserves/creates the button config.

The installer does not kill a manually started instance. Stop that instance before
enabling the service: foreground and daemon instances share an advisory lock, so
only one new-version process can own the virtual mouse. An older executable that
predates locking must be stopped explicitly. Reinstalling replaces the managed unit
atomically and preserves drop-ins and `daemon.env`. It explicitly restarts the user
service after deployment, because `enable --now` alone leaves an existing process
on its old executable inode. Installation returns after checking service activity,
without waiting indefinitely for a remote; activity is not proof of mouse readiness.
Press **1+2** (or the red **SYNC** button), watch the connection/calibration journal,
and check `wiimote-motionplus --status`. Keep the remote still during calibration.

The installer creates `${XDG_CONFIG_HOME:-$HOME/.config}/wiimote-motionplus/config.json`
from **`config.default.json`** only when no entry exists. All normal mappings are
included: A left click, B right click, 1 calibrate, 2 middle click, HOME pause,
D-pad scroll, and +/- sensitivity. Existing config bytes and symlinks (even
dangling ones) are preserved; reinstallation does not reset your remapping.
Config publication is non-overwriting and atomic, including concurrent installers.
Builds use temporary storage rather than generating a source-tree binary, and
executable replacement is atomic so a running old instance can continue unchanged.
Use `./install.sh --help` for options; unknown arguments fail before setup.

The `./wiimote-mouse` commands below refer to the optional **local development
build** above. After installation, use `wiimote-motionplus` instead.

## Usage and mode selection

Connect the remote using your existing Bluetooth setup. For the inherited
interactive connection workflow, explicitly opt in and press 1+2 as prompted:

```sh
./wiimote-mouse --connect --mode auto
```

Building and ordinary one-shot runs do not connect Bluetooth automatically.
Daemon mode **does** automatically discover/trust/connect Wii remotes by default.

```sh
./wiimote-mouse                         # auto: prefer MotionPlus
./wiimote-mouse --mode gyro             # require MotionPlus; never silently use tilt
./wiimote-mouse --mode accel            # original tilt-to-move behavior
./wiimote-mouse --mode gyro --gyro-sensitivity 10 --gyro-deadzone 0.5
```

Without `-d`/`--daemon`, discovery waits at most 15 seconds for input-node enumeration. `auto` allows
MotionPlus to appear during that window before falling back to an accelerometer.
A complete gyro/button pair starts immediately. `accel` starts as soon as an
accelerometer/button pair is available. Explicit `gyro` fails clearly if no
MotionPlus/button pair appears. The selected mode and input paths are printed.
There is no hot-switch between modes after startup; restart after attaching or
removing MotionPlus. With multiple remotes, the first complete gyro-capable group
is preferred, otherwise the first complete accelerometer group. No remote-selector
flag is provided.

Nodes are grouped by the shared HID parent from `/proc/bus/input/devices`'s
`S: Sysfs` path (removing `/input/inputN`). This matters because real MotionPlus
nodes may have empty `U: Uniq` and `P: Phys`. Uniq or physical identity is used only
as a fallback when no usable sysfs parent exists; unidentified nodes are ignored,
not mixed across controllers.

### Persistent daemon and user service

```sh
wiimote-motionplus -d                       # foreground; wait/reconnect forever
wiimote-motionplus --daemon --mode gyro     # require MotionPlus even while waiting
systemctl --user status wiimote-motionplus.service
journalctl --user -u wiimote-motionplus.service -f
systemctl --user stop wiimote-motionplus.service    # before a manual foreground run
systemctl --user start wiimote-motionplus.service
systemctl --user disable --now wiimote-motionplus.service  # turn off autostart
```

`-d` and `--daemon` do not fork/background themselves. They stay in the foreground
for systemd (or another supervisor), initially accepting an already-connected remote.
When absent they poll every 200 ms without repeated waiting logs. After a disconnect,
read error, or `SYN_DROPPED`, both readers are canceled and joined, both physical nodes
are closed, and the old uinput device is destroyed **before** scanning again. Each
connection gets newly enumerated paths, initial axes, runtime/button state, and fresh
stationary gyro calibration. Keep the remote still for one second on every connection.
Open/permission/enumeration races retry with a 200 ms to 2 s backoff; identical session
errors are logged at most once per 10 seconds. A successful initialized session logs
its mode/paths. Ctrl+C or SIGTERM cancels waiting, startup, and active sessions.

Daemon `auto` waits up to **one second after an accelerometer/button pair first
appears** for late MotionPlus enumeration, then starts tilt mode. A complete gyro pair
starts immediately. Explicit daemon `gyro` waits indefinitely, never falling back to
tilt. There is no live mode switch inside a session. The one-shot 15-second discovery
behavior above is unchanged.

**Automatic Bluetooth connection is enabled in daemon mode.** While no suitable
input pair exists, an independent cancellable worker powers on the adapter once,
uses bounded `bluetoothctl` commands, rotates across cached remotes, and performs bounded discovery even when
cached remotes are offline. Only exact names `Nintendo RVL-CNT-01` and
`Nintendo RVL-CNT-01-TR`, with strict six-octet MAC addresses, are eligible for
**automatic trust and connection**. Names are an allowlist, not cryptographic device
authentication: run discovery only when nearby Wii remotes are acceptable to trust.
The worker never restarts the Bluetooth service, and is canceled/joined as soon as
suitable input nodes appear; input polling is not blocked by connect timeouts.
Failed attempts back off from 2 to 15 seconds in addition to bounded command times,
without repetitive failure logs. Ctrl+C/SIGTERM terminates connector processes.

Bluetooth cannot power on the controller. Press **1+2** or **SYNC** until its LEDs
blink; repeat if the discovery window expires. A working adapter/BlueZ and the HID
system setup remain required. Use `--daemon --auto-connect=false` to retain only
existing-link evdev reconnection. `--connect` remains a one-shot workflow; stop the
service first. `--daemon --connect` remains rejected before hardware access.

### Remote information and battery

```sh
wiimote-motionplus --status              # safe alongside the active daemon
wiimote-motionplus --daemon --battery-warning 20 --battery-interval 60s
wiimote-motionplus --daemon --battery-warning 0   # disable low-battery popups
```

`--status` reads recognized connected input devices and sysfs information without
opening input nodes, creating a mouse, loading button config, taking the instance
lock, or connecting Bluetooth. It prints a clear message when no remote is connected.
Information includes identity, Bluetooth MAC when available, model/vendor/product,
and kernel-reported battery percentage (or unavailable). Battery estimates are not
precision measurements and some kernels/devices expose none.

Each initialized input session starts a separate battery monitor and stops/joins it
on session teardown. The default warning threshold is **20%**, polling every **60s**;
threshold range is 0–100 (0 disables popups), interval minimum is 5s. Missing sysfs
battery data or notification errors do not stop mouse input. Low-battery warnings
are deduplicated rather than emitted on every poll. The default installer installs
optional `notify-send` when absent: `libnotify` on Arch/Fedora, `libnotify-bin` on
Debian/Ubuntu, `libnotify-tools` on openSUSE. An available command causes no package
installation. A desktop notification service/session bus is also needed; failures
remain visible in the journal. `--user-only` does not install packages. For custom
service flags, use a systemd user drop-in, preserving the existing mode/sensitivity
arguments when overriding `ExecStart`.

The shared lock is `$XDG_RUNTIME_DIR/wiimote-motionplus/instance.lock`, falling back to
`${XDG_CACHE_HOME:-$HOME/.cache}/wiimote-motionplus/instance.lock` in a private,
user-owned directory. It is an advisory `flock`, released by process exit; its empty
file may remain and must **not** be deleted to try to unlock a running instance.
Use the same runtime/cache environment for manual and service instances.

The user unit is installed in `${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user`.
The installer pins its `XDG_CONFIG_HOME` to the installation's config root (including
spaces/percent signs), so the service reads the same button config as the installer.
The user manager must use that same XDG unit search root; if your shell and user manager
have different XDG settings, align them before installation. The repository's static
unit uses the normal `%h/.config` default for manual installation. The unit uses
`Type=exec`, `Restart=on-failure`, a 2-second restart delay, and `default.target`;
it has no system Bluetooth dependency in the user manager. Enabling it means autostart
with your user session, not necessarily before login. The installer does not enable
lingering or otherwise change system login policy.

Service defaults are `auto` mode and gyro sensitivity **10**, unchanged from the CLI.
To preserve a preferred sensitivity (for example 35), create/edit the optional
`${XDG_CONFIG_HOME:-$HOME/.config}/wiimote-motionplus/daemon.env`:

```ini
WIIMOTE_MODE=auto
WIIMOTE_GYRO_SENSITIVITY=35
```

Then run `systemctl --user restart wiimote-motionplus.service`. This is a systemd
EnvironmentFile, not a shell script: do not use `export` or shell commands. Values
are passed as individual arguments without a shell. The installer never creates or
overwrites `daemon.env`. Button-adjusted sensitivity is session-local and resets to
the CLI/service value on reconnect. For other settings use a user drop-in via
`systemctl --user edit wiimote-motionplus.service`, not edits to the managed unit.

Invalid button config at startup or before a reconnection is fatal with exit **78**,
not a transient device retry. No stale mappings are reused. The service deliberately
does not restart on exit 78 (or invalid CLI arguments, exit 2): fix the config/settings
and run `systemctl --user restart wiimote-motionplus.service`. A missing *automatic*
button config still means built-in defaults, as in foreground mode.

### Gyro calibration and feel

**Keep the remote stationary for at least one second after startup**, while a
window of at least 20 calibration samples is collected after the first input frame.
Do the same after
pressing **1** to recalibrate. Movement is suppressed while calibrating. Calibration
averages the stationary yaw/pitch readings to estimate bias; it does not treat the
last angular velocity as a center or infer an absolute pointing direction. Moving
during calibration gives a bad bias: stop moving and press 1 again.

Gyro mode integrates angular velocity over elapsed time, retaining fractional
pixels. Yaw moves X and pitch moves Y; roll is ignored. Holding the remote at a
fixed tilt does **not** continuously move the pointer as it does in accel mode.
No sensor bar, IR tracking, or raw HID initialization is used. Opening the kernel
MotionPlus node activates its gyroscope.

- `--gyro-sensitivity`: pixels per degree, default **10**, finite and greater than
  zero, maximum 100. `+` / `-` changes it by 1 px/degree (button-adjustable minimum 0.1).
- `--gyro-deadzone`: degrees/second, default **0.5**, finite range 0–100. Rates below
  the threshold are zero; the threshold is subtracted from larger rates.
- Accelerometer sensitivity retains its original default 0.35 and +/- step 0.05.

The kernel already removes the MotionPlus 8192 center and normalizes fast/slow
ranges to `8192 * 9 / 440` units per degree/second. Its axes are `ABS_RX` yaw,
`ABS_RY` roll, and `ABS_RZ` pitch. See Linux's
`drivers/hid/hid-wiimote-modules.c` (`wiimod_mp_in_mp`, `wiimod_mp_probe`).

Initial axis state is read using `EVIOCGABS`, because evdev may omit unchanged
axes. Incomplete event records are retained across reads and axis changes commit
only on `SYN_REPORT`. During calibration, the last complete axis state is also
sampled at movement ticks: evdev can suppress unchanged stationary values and even
entire unchanged frames. No pointer movement occurs before any input frame. Once
calibrated, only real input frames refresh the gyro watchdog: gyro motion stops
after 100 ms without fresh frames and long tick gaps are not integrated. A long
calibration sampling gap restarts the averaging window. Legacy accelerometer mode
intentionally keeps moving while a tilt is held, even if its ABS values no longer
change; read errors/disconnects still terminate either mode. HOME clears fractional motion and
resets integration timing, so resuming does not integrate time spent paused.
EOF/read errors or `SYN_DROPPED` on either input source terminate safely and destroy
the virtual device rather than continuing on stale motion/buttons. After a dropped
event or disconnect, the one-shot program must be restarted. Daemon mode tears down
the entire session and reconnects automatically when compatible input nodes return.

## Button mapping

| Wii Remote | Action |
|---|---|
| A | Left click |
| B | Right click |
| 2 | Middle click |
| 1 | Gyro: restart stationary bias calibration; accel: current tilt becomes center |
| D-pad | Scroll (vertical and horizontal) |
| +/- | Increase/decrease sensitivity for the active mode |
| HOME | Pause/resume pointer movement and scrolling |

These are the built-in defaults. Clicks remain available while paused, matching
the original behavior. Ctrl+C exits and destroys the virtual uinput device.

### JSON button configuration

Use an explicit file (validated **before** Bluetooth connection or hardware discovery):

```sh
./wiimote-mouse --config ./config.example.json --mode gyro
```

The example intentionally swaps A/B: **A → right click, B → left click**.
It is not installed automatically. Without `--config`, the program reads
`$XDG_CONFIG_HOME/wiimote-motionplus/config.json`, or
`~/.config/wiimote-motionplus/config.json` when `XDG_CONFIG_HOME` is empty/unset.
A missing automatic file leaves the built-in defaults unchanged; a missing
explicit file is an error. An existing invalid/unreadable file is always an error.
The installer initializes this automatic path with normal mappings, never with
`config.example.json`. Edit the installed file to customize buttons. For a manual
setup without running the installer, create a default file without overwriting
any existing entry:

```sh
config_dir="${XDG_CONFIG_HOME:-$HOME/.config}/wiimote-motionplus"
mkdir -p "$config_dir"
(set -C; cat config.default.json > "$config_dir/config.json")
./wiimote-mouse --mode gyro
```

Files are UTF-8 JSON objects extended with **`//` line comments** (JSONC-like,
not strict JSON), while keeping the filename **`config.json`**. The generated
`config.default.json` and swap example contain English guidance, every assignable
button/action name, and inline binding explanations. Comments can occupy a whole
line or follow a value; they end at CR/LF or EOF. `//` inside double-quoted strings
is preserved, including escaped quotes/backslashes. External strict JSON tools
(such as `jq` or `json.Unmarshal`) need comment preprocessing; do not strip comments
with a naive regex that would damage strings or URLs. Only line comments are
supported: **`/* ... */` block comments and trailing commas are rejected**.

Override only the buttons you want to change;
unmentioned buttons keep their defaults. `{}` or `{"buttons": {}}` changes nothing.
For example, swap A/B and disable HOME:

```jsonc
{
  "buttons": {
    "a": "right_click", // A: right click
    "b": "left_click", // B: left click
    "home": "none" // Disable HOME; other buttons retain their defaults
  }
}
```

Button names (case-sensitive): `a`, `b`, `one`, `two`, `home`, `up`, `down`,
`left`, `right`, `plus`, `minus`. Any button can use any action below:

| Action | Behavior |
|---|---|
| `left_click`, `right_click` | Mouse button held until source button released (dragging supported) |
| `middle_click` | One middle-click tap on press, not a held button |
| `calibrate` | Gyro stationary-bias recalibration; accel current tilt becomes center |
| `pause` | Toggle pointer movement and scrolling; clicks still work |
| `scroll_up`, `scroll_down`, `scroll_left`, `scroll_right` | Repeated scroll while held; suppressed while paused |
| `sensitivity_up`, `sensitivity_down` | One adjustment on press, using the active mode's existing steps/limits |
| `none` | Disable this button |

Calibration, pause, sensitivity, and middle click run only on a fresh press.
Duplicate presses and evdev repeat events do not retrigger actions. Multiple
buttons may share a held click/scroll action: releasing one does not end the hold
until all sources are released. Button bindings do not affect gyro direction or
motion safety. Config is validated before any hardware/lock operation. In one-shot
mode it is read once at startup; edits require restarting. In daemon mode it is read
again before every new connection, not hot-reloaded during an active session.
Startup calibration prompts refer to the default **1** binding; when remapping,
use whichever button you assigned `calibrate`.

Unknown fields, buttons, actions, invalid JSON types (including `null`), and
trailing JSON documents/data are rejected with a config error. Supporting `//`
comments does not relax these checks. No shell commands or keyboard bindings are supported.

## Verification and hardware limitations

```sh
bash -n install.sh
go test -run '^TestInstaller' -v .
go test -race ./...
go vet ./...
```

Installer integration tests invoke the real Bash script and real Go builds with
isolated temporary HOME/XDG/TMPDIR directories and explicit real GOCACHE/GOPATH.
They exercise `--user-only --no-daemon`, default config loading through the Go loader,
custom-config preservation on reinstall, symlinks, argument validation, executable
`--help`, running-executable replacement, cleanup, and absence of new source-tree
binaries. System commands are guarded so these tests cannot invoke sudo or change
the host's Bluetooth/services/permissions. Full privileged system setup and actual
hardware behavior are **not** verified by these installer tests. Daemon installer
tests replace `systemctl` with an allowlisted fake and verify reload, enable/start/restart,
active-state checking, honest failure reporting, opt-out, unit escaping, and preservation
of `daemon.env`. No test mutates the real user manager. Static and generated units are
checked with `systemd-analyze --user verify` when available (verification substitutes a
test executable, so no real-HOME installation is required).

Automated tests cover realistic empty-identity sysfs grouping, mixed-controller
rejection, delayed gyro enumeration and auto fallback/gyro timeout, CLI validation,
averaged calibration, yaw/pitch integration and fractional carry, stale/paused motion,
recalibration, initial-axis snapshots, legacy tilt/button/scroll mappings, split
input records, disconnects, dropped events, and reader-to-runtime event dispatch.
Timing-sensitive motion/discovery tests use explicit/injected clocks. Daemon tests
also cover disconnected waiting beyond one-shot deadlines, late-gyro grace, reconnect
re-enumeration, config reload/fatal errors, bounded retry/logging, advisory lock
contention/release, real-pipe session cleanup, fresh initial axes, and SIGTERM subprocesses
while waiting and active. CLI subprocess tests prove invalid config fails immediately
before locks/hardware. Bluetooth startup cancellation is tested with a fake executable,
never the host's Bluetooth stack.

Real-hardware checks on one Wii Remote with MotionPlus confirmed usable gyro
pointer movement (including the horizontal direction correction), automatic
Bluetooth reconnection and resumed input under the same daemon process, and
sysfs identity/battery readout. This is not validation of every controller model.
Actual low-battery notification delivery, cold-boot behavior, and fresh privileged
setup on an unconfigured machine remain unverified. Automated tests and builds
alone do **not** establish physical axis direction, actual sampling cadence,
Bluetooth behavior, or pointer feel. Validate your own hardware before relying on it:

1. Start with `--mode gyro`, keep the remote still during calibration, and confirm
   that the logged motion node is `Nintendo Wii Remote Motion Plus`.
2. Rotate horizontally and vertically, then roll: yaw/pitch should move the
   corresponding axes, while roll alone should not. Check direction and tune
   sensitivity/deadzone for the display and grip.
3. Stop at a tilted orientation: there should be no sustained movement. If there
   is drift, keep still and press 1. Recheck after the remote warms up.
4. Check every button, HOME pause/resume, recalibration, and disconnect/reconnect.
5. Check `--mode accel` on a legacy remote, `auto` fallback without MotionPlus,
   and that `--mode gyro` reports missing hardware instead of falling back.
