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

resource "linuxhost_package" "curl" {
  name = "curl"
}

resource "linuxhost_package" "git" {
  name = "git"
}

resource "linuxhost_directory" "apps" {
  path  = "/opt/apps"
  mode  = "0755"
  owner = "root"
  group = "root"
}
