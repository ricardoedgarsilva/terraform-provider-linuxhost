# linuxhost_directory

Creates and manages a directory on the remote host.

## Example Usage

```terraform
resource "linuxhost_directory" "data" {
  path  = "/srv/data"
  mode  = "0755"
  owner = "1000"
  group = "1000"
}
```

## Schema

### Required

- `path` (String) Absolute path on the remote host. Changing this value replaces the resource.

### Optional and Computed

- `group` (String) Numeric GID or group name.
- `mode` (String) Octal permission string. Defaults to `0755`.
- `owner` (String) Numeric UID or username.

## Behavior

The provider creates the directory with `mkdir -p`, applies the requested mode,
and optionally applies ownership. Read operations refresh the mode, owner, and
group from the remote host.

Destroying this resource intentionally does not remove the directory from disk,
because homelab directories often contain service data. Removing the resource
from Terraform configuration removes it from state only.
