# web

Vue 3 + Vite + Pinia SPA (account home + chat layout). Synthetic viewer data only; no membership UI.

```bash
npm install
npm run dev          # proxies /v1 to :20365
npm run build        # Docker/H5: base `/` → dist/  (cmd/server serves this if present)
npm run build:port   # same base `/` → dist/port
npm run build:cgi    # fnOS iframe: base `/cgi/ThirdParty/WxBackup/index.cgi/` → dist/cgi
npm test
```

`build` / `build:port` use `vite --base /`. `build:cgi` is `vite --base /cgi/ThirdParty/WxBackup/index.cgi/`.

The Go server looks for `web/dist/index.html` (override with `WXBACKUP_WEB`). If that file is missing, it stays API-only and does not fail.
