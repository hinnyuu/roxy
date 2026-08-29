// Package parser 负责文件名/目录解析：规则引擎先行（发布组命名模板、
// 中括号提取、集数模式、version_key 归一化、字幕语言标签映射），
// 解析失败或低置信时由 LLM parse schema 兜底。
// 设计见 docs/ARCHITECTURE.md §6、§9 与 docs/DECISIONS.md D-012/D-013。
package parser
