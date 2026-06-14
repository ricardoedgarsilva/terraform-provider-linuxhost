# Linuxhost Provider

The Linuxhost provider manages small Linux hosts over SSH. It is intended for
homelab machines where Terraform should manage host baseline resources such as
packages, directories, files, cron jobs, and synced application folders.

This provider is currently Ubuntu-only. Other Linux distributions are not
supported yet.

## Example Usage

```terraform
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
```

## Schema

### Required

- `host` (String) IP address or hostname of the Linux host. SSH uses port 22.
- `private_key` (String, Sensitive) PEM-encoded SSH private key.
- `user` (String) SSH username.

### Optional

- `sudo` (Boolean) Prefix supported remote commands with `sudo`.

## Security Notes

The provider currently uses insecure SSH host key verification. This is useful
for homelab iteration, but should be improved before relying on the provider in
less trusted environments.

Remote commands are built for trusted Terraform configurations. Avoid passing
untrusted input into provider or resource arguments.

## Maintenance

This provider was created in the author's free time to manage a personal
homelab. It is shared in case it is useful, but is not actively maintained and
does not come with support, fixes, new features, or compatibility guarantees.
