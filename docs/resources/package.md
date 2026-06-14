# linuxhost_package

Installs and removes Ubuntu packages with `apt-get`.

## Example Usage

```terraform
resource "linuxhost_package" "docker" {
  name = "docker.io"
}
```

## Schema

### Required

- `name` (String) Package name to install. Changing this value replaces the resource.

### Read-Only

- `version` (String) Installed package version reported by `dpkg-query`.

## Behavior

On create, the provider runs `apt-get install -y` with `DEBIAN_FRONTEND=noninteractive`.
Package operations are guarded by a lock at `/tmp/homelab-apt.lock` to avoid
concurrent apt runs from this provider.

On read, the provider uses `dpkg-query` to refresh the installed version. If the
package is missing, the resource is removed from state.

On destroy, the provider runs `apt-get remove -y`.
