# rosup

rosup is for people who still run a small or medium RouterOS 6 network.

It downloads Long-term packages, upgrades one device at a time, checks the result, and writes a text audit.

> ⚠️ RouterOS 7 and other channels are not supported.

## Architecture

Three places matter.

**Git** is the ops repo. The checkout lives at `ops.path` and the remote is `ops.remote`. You commit `inventory/` so the controller knows which boxes exist and which role each one has. `rosup pull` force-syncs that checkout from the remote (remote always wins). After an upgrade finishes, rosup writes `audit/` so you can read what changed without mixing that into inventory. Periodic `backup run` writes redacted text exports under `backups/`. The GitHub deploy key is ed25519.

**Managed devices** are the rows in `inventory/devices.yaml`. Each row is a name, address, port, role, validation profile, group, order, and optional depends_on. That file is the fleet. rosup does not scan the network. Upgrade order is group, then order, then name.

**RouterOS** runs on those devices. The controller logs in as user `rosup` over SSH with an RSA key. That is the OS being upgraded. One device at a time, so a failure stops the run before the next box.

The controller itself is a trusted host with a static address. Devices allow that address only.

## Install

Download `rosup-linux-amd64` from a [GitHub Release](https://github.com/taihen/rosup/releases) and put it on `PATH`. The image `ghcr.io/taihen/rosup` runs as uid `65532`. Mount every path from the config into the container.

Pass `--config`, or set `ROSUP_CONFIG`. If neither is set, rosup reads `./rosup.yaml`.

```bash
rosup version
```

## Keys

> ⚠️ `ssh-keygen` rosup runs unattended, so the keys carry no passphrase.

```bash
mkdir -p /var/lib/rosup/ssh
ssh-keygen -t rsa -b 4096 -N '' -C rosup-controller -f /var/lib/rosup/ssh/id_rsa
ssh-keygen -t ed25519 -N '' -C rosup-ops -f /var/lib/rosup/ssh/id_ed25519_ops
chmod 700 /var/lib/rosup/ssh
chmod 600 /var/lib/rosup/ssh/id_rsa /var/lib/rosup/ssh/id_ed25519_ops
```

The RSA pair is the RouterOS user key. The ed25519 pair is the GitHub deploy key for the ops repo. Those paths are the ones in [Config](#config). Add `id_ed25519_ops.pub` as a deploy key on the ops repo. In the container, uid `65532` must be able to read the files.

Record GitHub host keys for `ops.git_known_hosts_path` so git does not prompt:

```bash
ssh-keyscan github.com > /var/lib/rosup/ssh/github_known_hosts
```

## Create the device user

Upload `id_rsa.pub` to the device first.

> RouterOS 6.49 rejects ed25519 user keys.

From an admin session:

```
/user add name=rosup group=full password=... address=CONTROLLER_IPV4/32 comment="rosup-controller"
/user ssh-keys import user=rosup public-key-file=id_rsa.pub
```

RouterOS requires a password on `/user add`. rosup logs in with the key. If failed logins put the controller on the `/ip ssh` blacklist, clear it from an admin session before retrying.

The first successful SSH stores the host key. A later mismatch is a hard failure. Remove the stale `known_hosts` entry after you confirm the device yourself.

## Prepare the ops repo

Point `ops.path` at an ops git checkout:

```
inventory/devices.yaml
inventory/profiles/<role>.yaml
audit/
backups/
```

Commit `inventory/` yourself. rosup never commits that path. Run `rosup pull` to refresh the checkout from `ops.remote`; remote wins and local drift is discarded. `audit/` is written when an upgrade completes. `backups/` holds dated text exports from `backup run`. Do not edit those machine-written trees.

### devices.yaml

`inventory/devices.yaml` is the fleet. One YAML list under `devices:`. Each entry is one RouterOS box. rosup does not discover hosts. If a box is missing from this file, it is not managed.

```yaml
# Human-edited. rosup never commits this path.
devices:
  - name: core-1
    address: 192.0.2.1
    port: 60022
    role: ospf
    group: core
    validation_profile: ospf
    order: 10
  - name: edge-1
    address: 192.0.2.10
    port: 60022
    role: radio
    group: radio
    validation_profile: radio
    order: 10
    depends_on: [core-1]
```

SSH username, keys, TOFU, timeouts, and the default port live in `rosup.yaml`. They are not device fields.

#### Device fields

**name** (required)

Inventory id and RouterOS identity. Letters, digits, `.`, `_`, and `-` only. Pattern: `^[A-Za-z0-9._-]+$`. Must be unique. `discover` requires the device identity to match this name.

**address** (required)

Host or IP for SSH. Must be unique together with `port`. Two devices may not share the same address and port. If `port` is omitted (treated as 0), that address also collides with any other entry that uses the same address on an explicit port.

**port** (optional)

SSH port for this device. Omit it to use `ssh.default_port` from config (22 unless you set something else).

**role** (required)

What kind of box this is. Must be one of:

- `ospf`
- `pppoe`
- `radio`
- `switch`
- `access`

**validation_profile** (required)

Which post-upgrade checks to run. Same allowed values as `role`. Almost always the same string. Use a different value only when the check profile should differ from the role label. The profile file is `inventory/profiles/<validation_profile>.yaml`.

**group** (optional)

Batch label for `plan`, `upgrade`, `discover`, `status`, and `backup run --group`. Empty is allowed. Sort key with `order` and `name`.

**order** (optional)

Integer inside a group. Lower numbers run first. Default is 0 when omitted. Upgrade and plan order is group (string sort), then order, then name.

**depends_on** (optional)

List of other device `name` values that must finish this release before this device upgrades. Each name must exist in the same file. A device may not depend on itself. Cycles are rejected at load time.

Each dependency must already be complete on that release, or upgrade earlier in the same run. Unmet deps show as BLOCKED in `plan`.

#### Load rules

Inventory load fails if any of these hold:

- duplicate `name`
- empty `address`
- unknown `role` or `validation_profile`
- shared address/port as described under `address`
- bad `name` charset
- `depends_on` pointing at a missing name, self, or a cycle

#### Profiles

Each distinct `validation_profile` you use needs `inventory/profiles/<name>.yaml`.

Required:

- `convergence_timeout`: Go duration string such as `5m`. Must be positive. Wait starts after SSH is back, not during the reconnect budget (default 3m).

Optional:

- `neighbor_state_allow`: OSPF neighbor states to accept. Default is `Full` and `2-Way`.
- `route_count_tolerance`: how many extra OSPF routes above baseline are allowed. Default 0.
- `session_restore_timeout`: extra wait for PPPoE sessions. Use with a longer `convergence_timeout` for PPPoE.

Typical starting points: radio `5m`, PPPoE `10m` plus `session_restore_timeout`, switch and access a couple of minutes. Do not upgrade a console router with almost no free disk.

#### Hardware notes

Devices that report `routerboard: no` from `/system routerboard print` are supported for RouterOS package upgrades. rosup skips the RouterBOOT update and reboot stages for them. RouterBOARD devices, and legacy output without the `routerboard` marker, must report both `current-firmware` and `upgrade-firmware`.

## Config

```yaml
data_dir: /var/lib/rosup
state_dir: /var/lib/rosup/state
package_dir: /var/lib/rosup/packages
backup_dir: /var/lib/rosup/backups
lock_path: /var/lib/rosup/state/rosup.lock
architectures:
  - arm
channel: long-term
ops:
  path: /var/lib/rosup/ops
  remote: git@github.com:ORG/rosup-ops.git
  ssh_private_key_path: /var/lib/rosup/ssh/id_ed25519_ops
  git_known_hosts_path: /var/lib/rosup/ssh/github_known_hosts
ssh:
  private_key_path: /var/lib/rosup/ssh/id_rsa
  known_hosts_path: /var/lib/rosup/ssh/known_hosts
  default_port: 60022
```

`channel` must be `long-term`. `architectures` must list every arch you sync. Username defaults to `rosup`. `ssh.default_port` defaults to 22. Set `60022` if that is the device port. TOFU defaults to true. Reconnect defaults to 3m and 3 attempts. If `ops.ssh_private_key_path` is set, `ops.git_known_hosts_path` is required. That file is GitHub host keys, never the RouterOS TOFU file. The Go binary has no shell, git, or ssh.

## Upgrade

Prove SSH once:

```bash
rosup discover
rosup discover --group GROUP
```

Each Long-term release:

```bash
rosup release sync
rosup plan --release VERSION
rosup upgrade --release VERSION --group GROUP
```

`plan` prints the readiness report on stdout. If any host is blocked (disk, missing packages, unmet `depends_on`, or unsupported), it exits non-zero with a short counts-only error on stderr (host details stay on stdout).

`release sync` prints `synced VERSION (N files)`. Use that VERSION. Omit `--group` to do every device. One device at a time. The first failure stops the run. A device already on the release skips packages and reboot. Incomplete jobs need `--resume` for the same release.

```
>  edge-1  checking SSH and version
*  edge-1  checking SSH and version
-  edge-1  already VERSION
x  edge-1  installing packages
```

`>` started, `*` done, `-` skipped, `x` failed. Errors on stderr.

## If it fails

Only one controller may run. A second instance fails until the lock is released.

```bash
rosup pull
rosup status
rosup status --group GROUP --release VERSION
rosup verify DEVICE
rosup upgrade --release VERSION --group GROUP --resume
rosup rollback DEVICE --to-version VERSION
rosup backup run
rosup backup run --group GROUP
rosup backup restore DEVICE --file PATH
```

`rosup pull` fetches `ops.remote` (with prune), requires the current branch to track `origin`, then cleans untracked paths and hard-resets to that tip. Uncommitted edits, untracked files, unpushed local commits, and other local-only worktree files are discarded. This is not `git pull`: remote always wins. Soft refreshes inside `backup run` / upgrade audit pushes still use a normal fast-forward pull and do not discard local commits. If pull fails after the clean step, re-run it; do not hand-edit the checkout to recover.

`backup run` takes a local binary backup and text export per device, prunes old files under `backup_dir` by `backup_retention_days`, then commits redacted `.rsc` files to the ops repo at `backups/<device>/YYYY/MM/<device>-<timestamp>.rsc`. Failed devices are skipped for the git push; the command exits non-zero if any device or the push failed. Suitable for cron.

`status` joins inventory with local job state. After a fix, resume the same release; do not change `--release` mid-job. With `--resume`, only failed or in-progress jobs for that release continue; pending and untouched devices are skipped.

## License

[MIT](LICENSE)
