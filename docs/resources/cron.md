# linuxhost_cron

Manages a cron entry as a file under `/etc/cron.d`.

The remote file path is `/etc/cron.d/homelab-<name>`.

## Example Usage

```terraform
resource "linuxhost_cron" "backup" {
  name    = "backup"
  minute  = "0"
  hour    = "3"
  user    = "root"
  job     = "/usr/local/bin/backup"
}
```

## Schema

### Required

- `job` (String) Command to execute.
- `name` (String) Unique job name. Used as the suffix for the file in `/etc/cron.d`. Changing this value replaces the resource.

### Optional

- `day` (String) Day-of-month field. Defaults to `*`.
- `hour` (String) Hour field. Defaults to `*`.
- `minute` (String) Minute field. Defaults to `*`.
- `month` (String) Month field. Defaults to `*`.
- `user` (String) User to run the job as. Defaults to `root`.
- `weekday` (String) Weekday field. Defaults to `*`.

## Behavior

On create and update, the provider writes a single cron line to `/etc/cron.d`.
On read, it parses the first non-comment line in that file. On destroy, it
removes the managed cron file.
