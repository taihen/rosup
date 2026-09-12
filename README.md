# rosup

rosup is for people who still run a small or medium RouterOS 6 network.

It downloads Long-term packages, upgrades one device at a time, checks the result, and writes a text audit.

> ⚠️ RouterOS 7 and other channels are not supported.

## Architecture

Three places matter.

**Git** is the ops repo. The checkout lives at `ops.path` and the remote is `ops.remote`. You commit `inventory/` so the controller knows which boxes exist and which role each one has. After an upgrade finishes, rosup writes `audit/` so you can read what changed without mixing that into inventory. The GitHub deploy key is ed25519.

**Managed devices** are the rows in `inventory/devices.yaml`. Each row is a name, address, port, role, group, and order. That file is the fleet. rosup does not scan the network. Upgrade order is group, then order, then name.

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
```

Commit `inventory/` yourself. rosup never commits that path. `audit/` is written when an upgrade completes. Do not edit it.

```yaml
# Human-edited. rosup never commits this path.
devices:
  - name: edge-1
    address: 192.0.2.10
    port: 60022
    role: radio
    group: radio
    validation_profile: radio
    order: 10
```

`role` and `validation_profile` must each be `ospf`, `pppoe`, `radio`, `switch`, or `access`. They are usually the same. Each used role needs `inventory/profiles/<role>.yaml` with `convergence_timeout`. That wait starts after SSH is back, not during the 3m reconnect. Radio is typically 5m. PPPoE is 10m plus `session_restore_timeout`. Do not upgrade a console router with almost no free disk.

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

`plan` prints the readiness report on stdout. If any host is blocked, it exits non-zero with a short counts-only error on stderr (host details stay on stdout).

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
rosup status
rosup status --group GROUP --release VERSION
rosup verify DEVICE
rosup upgrade --release VERSION --group GROUP --resume
rosup rollback DEVICE --to-version VERSION
rosup backup restore DEVICE --file PATH
```

`status` joins inventory with local job state. After a fix, resume the same release; do not change `--release` mid-job. With `--resume`, only failed or in-progress jobs for that release continue; pending and untouched devices are skipped.

## License

[MIT](LICENSE)
