// Package web 内嵌前端构建产物（D-028/D-041）。
// 默认（无 build tag）嵌入占位页——无 node 环境 `go build ./...` 恒绿；
// 带 `-tags=web` 时嵌入 web/dist（nix build 经 buildNpmPackage 注入）。
package web
