# Backup and recovery

The blog creates an ordinary ZIP containing SQLite snapshots, uploads, configuration files, and optional local assets and TLS files. Database snapshots include content, history, image metadata, accounts, and sessions. Shared databases are copied once.

Configuration files are copied unchanged. **Save environment overrides separately**, especially signing keys and credentials. Remote images remain URLs. Their contents are not downloaded.

## Create and verify

Build the command from the blog source directory:

```sh
go build -o /tmp/blog-maintenance ./cmd/blog-maintenance
```

Stop the blog and other processes that write its data. From the application's configuration directory, run:

```sh
/tmp/blog-maintenance backup /backups/blog.zip
/tmp/blog-maintenance verify /backups/blog.zip
```

The destination's parent directory must exist. Existing archives are never overwritten. Keep backups outside uploads, local assets, and web-served directories.

The server, image optimizer, and backup command share a process lock. Older binaries, SQL tools, and other applications do not honor the lock and must be stopped separately. Do not delete `.blog-maintenance.lock`. The operating system releases the lock when its process exits.

Verification checks the inventory, file checksums, SQLite integrity, and foreign keys. It does not load archived configuration, restore files, or run migrations. Backup verifies the archive before publishing it.

## Recover manually

1. Verify a trusted archive. Use a compatible blog binary.
2. Extract the ZIP into a new, private staging directory with an ordinary ZIP utility.
3. Create a separate, empty application directory. Copy `config.yaml` and `modules/blog/config.yaml` there.
4. Use `manifest.json` to place the database snapshots, uploads, and optional assets/TLS files. Each resource records its archive `Path` and original `Source`. Database `Roles` identifies blog, account, and session storage. If the locations change, update the configuration yourself. Do not copy snapshots over existing databases or reuse old `-wal` and `-shm` files.
5. Restore environment overrides. Review storage paths, public origin, listener, mail settings, and administrator email before starting the recovered application. Confirm login, content, and images before switching traffic.

The archive groups snapshots under `databases/`, image bundles under `uploads/`, local assets under `assets/`, and TLS files under `tls/`. These archive paths do not rewrite the configuration. There is no automated restore command.

## Limits

Archives contain private content and can contain credentials. The command requests owner-only permissions, but filesystems such as exFAT do not enforce them. Store backups on a filesystem that enforces permissions and check access. The archive is **not encrypted**. Checksums detect corruption, not a forged archive.

Backup requires local SQLite storage and regular files. Symlinks in file trees and unsupported SQLite options are rejected. The process lock currently supports Linux, macOS, and BSD, not Windows.

Archives are limited to 64 GiB of uncompressed data and 100,000 files/directories. Backup stages a copy beside the destination and needs space for that copy, the ZIP, and one database verification copy. Standalone verification uses the system temporary directory for one database at a time.

A machine failure can leave an empty destination or temporary files. Verify an interrupted backup before using it. Unsaved browser recovery copies, application binaries, scheduling, retention, and off-host transfer are not included.
