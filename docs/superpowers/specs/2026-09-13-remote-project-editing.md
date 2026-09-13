# Remote Project Connection Editing

Date: 2026-09-13
Status: Implemented

The project list stores remote projects with the legacy canonical `host` plus
structured fields: `remote_kind` (`ssh` or `wsl`), SSH user/host/port, or WSL
distro. Existing `host`/`path` entries are migrated when loaded.

`PATCH /api/projects/remote` updates a remote entry using `old_host` and
`old_path` as its identity. The request supplies `kind`, the corresponding
connection fields, and the new remote `path`. The operation validates SSH
ports (1–65535) and rejects ports for WSL, rejects identity collisions, and
preserves the project's display name, group, order, and timestamps.

Changing a remote identity immediately invalidates and terminates server-side
terminal sessions attached to the old `host:path` identity. The frontend keeps
the edited project active and new terminal connections use the updated target;
this prevents a stale terminal from reconnecting to the old host or path.

The project sidebar exposes **Edit connection** for SSH and WSL entries. SSH
fields are username, hostname/SSH alias, optional port, and remote path. WSL
fields are distribution and remote path.
