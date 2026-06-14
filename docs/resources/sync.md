# linuxhost_sync

Syncs a local directory to a remote directory over SSH.

This resource is useful for homelab application folders, including Docker
Compose stack directories.

## Example Usage

```terraform
resource "linuxhost_sync" "compose_stack" {
  source = "${path.module}/compose/whoami"
  dest   = "/opt/stacks/whoami"

  post_sync = "cd /opt/stacks/whoami && docker compose up -d --remove-orphans"
}
```

## Schema

### Required

- `dest` (String) Absolute path on the remote host. Changing this value replaces the resource.
- `source` (String) Local directory to mirror to the remote host.

### Optional

- `post_sync` (String) Shell command to run on the remote host after a successful sync.

### Read-Only

- `source_hash` (String) SHA256 hash of the source directory contents. Changes trigger a re-sync.

## Behavior

During planning, the provider computes a hash of the local source directory so
Terraform can detect file changes before apply.

On create and update, the provider streams the local directory as a tar archive
over SSH, creates the destination directory, and extracts the archive there.

If `post_sync` is set, the provider runs the command after a successful sync. If
provider-level `sudo` is enabled, `post_sync` is wrapped in `sudo bash -c` so
shell constructs such as `cd` and `&&` work as expected.

Destroying this resource intentionally does not remove the remote directory,
because synced application directories may contain service data. Removing the
resource from Terraform configuration removes it from state only.
