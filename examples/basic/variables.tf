variable "host" {
  type        = string
  description = "IP address or hostname of the Ubuntu host."
}

variable "user" {
  type        = string
  description = "SSH username."
}

variable "private_key_path" {
  type        = string
  description = "Path to the SSH private key used to connect to the host."
  default     = "~/.ssh/id_ed25519"
}
