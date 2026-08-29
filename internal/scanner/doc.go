// Package scanner 负责源发现：SourceProvider 接口与具体实现。
// v1: DirScanProvider（WebUI 手动触发 + 可选定时）；
// v2: QBittorrentProvider / TransmissionProvider。
// 设计见 docs/ARCHITECTURE.md §3 与 docs/DECISIONS.md D-025。
package scanner
