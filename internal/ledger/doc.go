// Package ledger 负责物料台账：产出物登记、返工回滚、漂移对账。
// 任何产出路径必须可经台账精确回滚（零破坏原则）。
// 设计见 docs/ARCHITECTURE.md §10 与 docs/DECISIONS.md D-010。
package ledger
