// Package organizer 负责物料化：创建相对软链接、生成 NFO、下载封面、
// 字幕版本配对。全部操作幂等且记入台账；文件名与 NFO 永远由同一
// placement 决策生成。设计见 docs/ARCHITECTURE.md §4/§9
// 与 docs/DECISIONS.md D-003/D-005/D-010/D-026。
package organizer
