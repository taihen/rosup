# rosup

rosup upgrades MikroTik RouterOS 6 Long-term. It syncs packages, upgrades one device at a time, validates by role, and writes text audit history. RouterOS 7 and other channels are not supported.

## Install

Download `rosup-linux-amd64` from a [GitHub Release](https://github.com/taihen/rosup/releases) and put it on `PATH`. Pull `ghcr.io/taihen/rosup` as uid `65532` and bind-mount every path in the config. Config is `--config`, else `ROSUP_CONFIG`, else `./rosup.yaml`; a missing file is fatal.

```bash
rosup version
```

## Prepare the device

Upload an RSA public key first. RouterOS 6.49 rejects ed25519 user keys. From an admin session:

```
/user add name=rosup group=full password=... address=CONTROLLER_IPV4/32 comment="rosup-controller"
/user ssh-keys import user=rosup public-key-file=rosup.pub
```

RouterOS requires a password on `/user add`. rosup logs in with the key. If failed logins put the controller on the `/ip ssh` blacklist, clear it from an admin session before retrying.

The first successful SSH stores the host key. A later mismatch is a hard failure; remove the stale `known_hosts` entry after you confirm identity out of band.

## Prepare the ops repo

Point `ops.path` at an ops git checkout:

```
inventory/devices.yaml
inventory/profiles/<role>.yaml
audit/
```

Commit `inventory/` yourself; rosup never commits that path. `audit/` is written when an upgrade completes; do not edit it. The GitHub deploy key is ed25519, separate from the RouterOS RSA key.

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

`role` and `validation_profile` must each be `ospf`, `pppoe`, `radio`, `switch`, or `access` (usually the same). Each used role needs `inventory/profiles/<role>.yaml` with `convergence_timeout` (starts after SSH reconnect, not during the 3m reconnect; radio typically 5m; pppoe 10m plus `session_restore_timeout`). Do not upgrade a console router with almost no free disk. Upgrade order is group, then order, then name.

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

`channel` must be `long-term`. `architectures` must be non-empty; list every arch you sync. Username defaults to `rosup`. `ssh.default_port` defaults to 22; set `60022` if that is the device port. TOFU defaults to true. Reconnect defaults to 3m / 3 attempts. If `ops.ssh_private_key_path` is set, `ops.git_known_hosts_path` is required (GitHub host keys, never the RouterOS TOFU file). The Go binary has no shell, git, or ssh.

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

`release sync` prints `synced VERSION (N files)`. Use that VERSION. Omit `--group` to do every device. One device at a time; the first failure stops the run. A device already on the release skips packages and reboot.

```
>  edge-1  checking SSH and version
*  edge-1  checking SSH and version
-  edge-1  already VERSION
x  edge-1  installing packages
```

`>` started, `*` done, `-` skipped, `x` failed. Errors on stderr.

## If it fails

Only one controller may run; a second instance fails until the lock is released.

```bash
rosup verify DEVICE
rosup rollback DEVICE --to-version VERSION
rosup backup restore DEVICE --file PATH
```

## License

[MIT](LICENSE)
