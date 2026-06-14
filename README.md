# Terraform Provider Linuxhost

`linuxhost` is a Terraform provider for managing small Linux hosts over SSH.
It is aimed at homelab-style machines where Terraform should handle the host
baseline: packages, directories, files, cron jobs, and synced application
folders.

The provider is currently Ubuntu-only while the project is still early. The
name leaves room for broader Linux distribution support later.

## Supported Distributions

| Distribution | Status |
| --- | --- |
| Ubuntu | Supported |
| Debian | Not currently supported |
| Fedora | Not currently supported |
| Arch Linux | Not currently supported |
| Alpine Linux | Not currently supported |
| Other Linux distributions | Not currently supported |

## Maintenance Disclaimer

I created this provider in my free time to manage my own homelab. It is shared
in case it is useful to others, but I am not actively maintaining it or
promising support, fixes, new features, or compatibility guarantees.

## Current Features

- Install and remove Ubuntu packages with `apt-get`
- Create and manage remote directories
- Write remote files without storing their contents in Terraform state
- Manage `/etc/cron.d` jobs
- Sync a local directory to a remote host
- Run an optional post-sync command, useful for Docker Compose stacks

## Example

```hcl
terraform {
  required_providers {
    linuxhost = {
      source = "ricardoedgarsilva/linuxhost"
    }
  }
}

provider "linuxhost" {
  host        = "192.168.1.10"
  user        = "ubuntu"
  private_key = file("~/.ssh/id_ed25519")
  sudo        = true
}

resource "linuxhost_package" "docker" {
  name = "docker.io"
}

resource "linuxhost_directory" "stacks" {
  path  = "/opt/stacks"
  mode  = "0755"
  owner = "root"
  group = "root"
}

resource "linuxhost_file" "env" {
  path    = "/opt/stacks/example/.env"
  mode    = "0600"
  content = "APP_SECRET=change-me\n"
}

resource "linuxhost_sync" "example_stack" {
  source = "${path.module}/stacks/example"
  dest   = "/opt/stacks/example"

  post_sync = "cd /opt/stacks/example && docker compose up -d --remove-orphans"
}

resource "linuxhost_cron" "backup" {
  name    = "backup"
  minute  = "0"
  hour    = "3"
  user    = "root"
  job     = "/usr/local/bin/backup"
}
```

## Provider Configuration

| Argument | Required | Description |
| --- | --- | --- |
| `host` | Yes | Hostname or IP address of the target machine. SSH uses port 22. |
| `user` | Yes | SSH username. |
| `private_key` | Yes | PEM-encoded SSH private key. This value is sensitive. |
| `sudo` | No | Prefix remote commands with `sudo` where supported. |

## Resources

### `linuxhost_package`

Installs an Ubuntu package using `apt-get install -y` and removes it with
`apt-get remove -y`.

```hcl
resource "linuxhost_package" "nginx" {
  name = "nginx"
}
```

### `linuxhost_directory`

Creates a remote directory and manages its mode and ownership. Destroying this
resource intentionally leaves the directory on disk, which is safer for service
data directories.

```hcl
resource "linuxhost_directory" "data" {
  path  = "/srv/data"
  mode  = "0755"
  owner = "1000"
  group = "1000"
}
```

### `linuxhost_file`

Writes a remote file and sets its mode. The `content` attribute is write-only
and is not stored in Terraform state. Reads intentionally remove the resource
from state so future applies rewrite the file.

```hcl
resource "linuxhost_file" "secret" {
  path    = "/etc/example/secret"
  mode    = "0600"
  content = var.secret
}
```

### `linuxhost_cron`

Writes a managed cron file under `/etc/cron.d`.

```hcl
resource "linuxhost_cron" "cert_renewal" {
  name    = "cert-renewal"
  minute  = "15"
  hour    = "2"
  user    = "root"
  job     = "certbot renew --quiet"
}
```

### `linuxhost_sync`

Copies a local directory to a remote directory by streaming a tar archive over
SSH. A hash of the local source is computed during planning so Terraform can
detect local file changes.

Destroying this resource intentionally leaves the remote directory in place.

```hcl
resource "linuxhost_sync" "compose_stack" {
  source = "${path.module}/compose/whoami"
  dest   = "/opt/stacks/whoami"

  post_sync = "cd /opt/stacks/whoami && docker compose up -d --remove-orphans"
}
```

## Development

Run the provider locally:

```sh
go run . -debug
```

Run the build checks:

```sh
go test ./...
```

## Security Notes

The provider currently uses `ssh.InsecureIgnoreHostKey()` while connecting to
hosts. That is convenient for homelab iteration, but host key verification
should be added before treating this as production-ready.

Remote commands are built for trusted Terraform configurations. Avoid passing
untrusted input into resource arguments.

## License

MIT License. See [LICENSE](LICENSE).
