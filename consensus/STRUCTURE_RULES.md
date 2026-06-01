# 共识算法目录结构规则

本规则用于统一 `hcap-consensus/consensus/` 下新增共识算法目录结构，避免出现非标准组织方式。

## 1. 目录命名

- 每个算法使用独立目录，目录名采用小写下划线风格。
- 一个目录只放一个算法实现，不混放多个算法。
- `common/` 仅放跨算法公共接口与通用能力，不放具体算法逻辑。

## 2. 必选文件

每个算法目录必须包含：

- `consensus.go`

`consensus.go` 负责：

- 暴露算法主结构与配置结构
- 实现 `common.ConsensusEngine` 接口

## 3. 标准模板

新增算法必须在以下两种模板中二选一：

### A. 完整模板（推荐）

- `consensus.go`
- `node.go`
- `message.go`
- `trust_scorer.go`
- `validator_selector.go`
- `tests/`（可选）

适用于需要节点行为建模、消息结构建模、评分/选择策略建模的算法。

### B. 轻量模板

- `consensus.go`
- `tests/`（可选）

适用于仅做实验指标建模或简化流程模拟的算法。

## 4. 一致性约束

- 若目录中出现 `node.go`、`message.go`、`trust_scorer.go`、`validator_selector.go` 中任意一个文件，则必须同时包含这四个文件。
- 测试代码统一放在 `tests/` 目录，不在算法根目录散放 `*_test.go`。
- 不在算法目录存放二进制、临时产物、实验输出文件。

## 5. 新增算法最小检查清单

- 新目录在 `consensus/` 下创建
- `consensus.go` 已实现 `Start/Stop/BeginBlock/EndBlock`
- 目录结构满足模板 A 或模板 B
- 如包含测试，测试位于 `tests/`
