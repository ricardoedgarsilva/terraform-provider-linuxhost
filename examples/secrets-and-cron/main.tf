terraform {
  required_providers {
    linuxhost = {
      source = "ricardoedgarsilva/linuxhost"
    }
  }
}

provider "linuxhost" {
  host        = var.host
  user        = var.user
  private_key = file(var.private_key_path)
  sudo        = true
}

resource "linuxhost_directory" "backup" {
  path  = "/etc/homelab-backup"
  mode  = "0750"
  owner = "root"
  group = "root"
}

resource "linuxhost_file" "backup_env" {
  path = "/etc/homelab-backup/backup.env"
  mode = "0600"

  content = templatefile("${path.module}/templates/backup.env.tftpl", {
    backup_repository = var.backup_repository
    backup_password   = var.backup_password
  })
}

resource "linuxhost_cron" "backup" {
  name   = "backup"
  minute = "0"
  hour   = "3"
  user   = "root"
  job    = ". /etc/homelab-backup/backup.env && /usr/local/bin/backup"
}
