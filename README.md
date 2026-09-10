# rosup

A portable Go CLI for MikroTik RouterOS 6 Long-term only. It syncs packages, upgrades one device at a time, validates by role, and writes text audit history.

RouterOS 7 and other channels are not supported.

## Install

Download the `rosup-linux-amd64` binary from a [GitHub Release](https://github.com/taihen/rosup/releases) and place it on your `PATH`.

Or pull the container image:

```bash
docker pull ghcr.io/taihen/rosup
```

The image runs as uid/gid `65532:65532` on a distroless base. There is no shell, `ssh`, or `git` inside the image.

## Configuration

rosup reads a YAML config file. Path resolution:

1. `--config` flag
2. `ROSUP_CONFIG` environment variable
3. `./rosup.yaml`

A missing config file is a fatal error. Paths in the config are not compiled into the binary.

Typical keys include `data_dir`, `state_dir`, `package_dir`, `backup_dir`, `lock_path`, `ops.path`, and SSH settings (`private_key_path`, `known_hosts_path`). `channel` must be `long-term`.

## Commands

| Command | Purpose |
| --- | --- |
| `rosup discover` | Read-only facts from a device |
| `rosup release sync` | Download Long-term packages to `package_dir` |
| `rosup release list` | List locally synced release versions |
| `rosup plan` | Show what an upgrade would do |
| `rosup upgrade` | Run the upgrade for a group |
| `rosup verify` | Re-check a device against its role profile |
| `rosup rollback` | Downgrade packages to a complete local release |
| `rosup backup restore` | Restore a local `.backup` file to a device |

`rosup version` prints the build version.

## Host keys (TOFU)

SSH host keys use trust on first use. The first successful connection stores the key in `known_hosts`. A later mismatch is a hard failure. To recover, remove the stale entry from the known_hosts file after you have confirmed the device identity out of band.

## Lockfile

Only one controller may run at a time. The lockfile is shared by the host binary and the container. Starting a second instance fails until the lock is released.

## Container mounts

Bind-mount every path the config references, owned by uid `65532`. At minimum:

| Config key | What to mount |
| --- | --- |
| `data_dir` | Working data; per-device baselines live in `data_dir/jobs` |
| `state_dir` | Per-device job state |
| `package_dir` | Synced `.npk` packages |
| `backup_dir` | Binary backups |
| `ops.path` | Inventory and audit git checkout |
| `ssh.private_key_path` | SSH private key |
| `ssh.known_hosts_path` | Known hosts file |
| `lock_path` | Controller lockfile (or its parent directory) |

Example:

```bash
docker run --rm \
  --user 65532:65532 \
  -v /var/lib/rosup/data:/data \
  -v /var/lib/rosup/state:/state \
  -v /var/lib/rosup/packages:/packages \
  -v /var/lib/rosup/backups:/backups \
  -v /var/lib/rosup/ops:/ops \
  -v /var/lib/rosup/ssh:/ssh \
  -v /path/to/rosup.yaml:/config/rosup.yaml:ro \
  ghcr.io/taihen/rosup \
  --config /config/rosup.yaml \
  version
```

Adjust host paths to match your layout. The process cannot write where uid `65532` lacks permission.

## License

[MIT](LICENSE)
