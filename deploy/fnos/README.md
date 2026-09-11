# fnOS FPK (`wxbackup`)

Packaging tree for a clean-room wxbackup install on 飞牛 fnOS. `fnpack` turns this directory into a `.fpk`.

Do **not** vendor ffmpeg or Chromium here. Media conversion (later PRs) should call a host `ffmpeg` if present.

## Layout

```text
deploy/fnos/
  manifest                 appname=wxbackup, source=thirdparty, service_port=20365
  ICON.PNG / ICON_256.PNG
  app/bin/wxbackup         Go server (built, not committed)
  app/ui/config            desktop iframe → /cgi/ThirdParty/wxbackup/index.cgi
  app/ui/index.cgi         CGI reverse-proxy to 127.0.0.1:20365
  cmd/main                 start | stop | status
  cmd/install_callback     dirs + chmod; warns if 8011/24011 are taken
  config/privilege         run-as=package (not root)
  config/resource          data-share wxbackup/data
  wizard/
```

## Ports

| Port | Role | Configurable? |
|------|------|----------------|
| 20365 | App HTTP (`WXBACKUP_PORT` / `TRIM_SERVICE_PORT`) | yes |
| 8011 | WeChat LAN discovery | **no** |
| 24011 | WeChat LAN transfer | **no** |

8011 and 24011 are above 1024, so the package user can bind them. `config/privilege` therefore uses `"run-as": "package"`, not root. Request root only if a future host policy blocks those binds. Keep both ports free of other apps and firewalls; otherwise LAN backup/restore fails while the viewer can still run.

Data lives in the `wxbackup/data` share (`TRIM_DATA_SHARE_PATHS` / `/var/apps/wxbackup/share/data`), passed to the binary as `WXBACKUP_DATA`.

## Build the Go binary

`platform=x86` in `manifest` matches `linux/amd64`. For ARM NAS, set `platform = arm` and build `linux/arm64`. Do not use `platform=all` when the payload contains an arch-specific binary.

From the repository root:

```bash
# x86_64 fnOS
mkdir -p deploy/fnos/app/bin
GOOS=linux GOARCH=amd64 CGO_ENABLED=1 \
  go build -o deploy/fnos/app/bin/wxbackup ./cmd/server

# ARM64 fnOS (separate FPK; change manifest platform=arm)
GOOS=linux GOARCH=arm64 CGO_ENABLED=1 \
  go build -o deploy/fnos/app/bin/wxbackup ./cmd/server
```

Cross-compiling cgo sqlite usually needs a matching cross-compiler. Building on the NAS (or in a Debian container for that arch) is simpler.

`index.cgi` is a Python 3 stub and does not need compiling.

## Pack with fnpack

Download [fnpack](https://developer.fnnas.com/docs/cli/fnpack/) for your machine, then:

```bash
chmod +x deploy/fnos/cmd/* deploy/fnos/app/ui/index.cgi
fnpack build --directory deploy/fnos
```

Or:

```bash
cd deploy/fnos
fnpack build
```

`fnpack` checks `manifest`, `config/privilege`, `config/resource`, `ICON.PNG`, `ICON_256.PNG`, `app/`, `cmd/`, and `wizard/`. The resulting `.fpk` is a gzip tar; install it from 应用中心 → 手动安装.

`.fpk` / `.tgz` artifacts are gitignored.

## Runtime

`cmd/main start` launches `${TRIM_APPDEST}/bin/wxbackup` with:

```text
WXBACKUP_PORT=20365
WXBACKUP_DATA=<data share or $TRIM_PKGVAR/data>
```

Desktop iframe hits `/cgi/ThirdParty/wxbackup/index.cgi`, which proxies to `http://127.0.0.1:20365`.
