// Package llm 实现 LLM Provider 抽象：OpenAI 兼容 Chat Completions 基座、
// 三个决策点 schema（parse/match/mapping）、结构化输出三级降级、
// 多 provider 降级与全量日志。设计见 docs/ARCHITECTURE.md §7
// 与 docs/DECISIONS.md D-016/D-017/D-018。
package llm
