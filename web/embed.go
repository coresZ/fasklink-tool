package web

import "embed"

//go:embed index.html app.js style.css vendor/qr.mjs
var FS embed.FS
