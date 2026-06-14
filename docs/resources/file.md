# linuxhost_file

Writes a file on the remote host.

The `content` attribute is write-only and is not stored in Terraform state.

## Example Usage

```terraform
resource "linuxhost_file" "secret" {
  path    = "/etc/example/secret"
  mode    = "0600"
  content = var.secret
}
```

## Schema

### Required

- `content` (String, Write-Only) File content to write. This value is not stored in Terraform state.
- `path` (String) Absolute path on the remote host. Changing this value replaces the resource.

### Optional

- `mode` (String) Octal permission string. Defaults to `0600`.

## Behavior

On create and update, the provider writes the requested content using `sudo tee`
and then applies the requested file mode.

Read operations intentionally remove the resource from state so every future
apply writes the configured content again. This is useful for secret files that
may rotate or drift outside Terraform without exposing their contents in state.

On destroy, the provider removes the file from the remote host.
