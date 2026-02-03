# HCP-Consensus

高频金融交易区块链共识性能测试系统 - 共识层

## 特性

- 基于 Cosmos-SDK v0.50 和 CometBFT v0.38
- 实现 tPBFT (信任弿PBFT) 共识机制
- 支持4节点测试网络
- Docker Compose 一键启动
- 支持 Raft 和 HotStuff 对比实验

## 快速启动

```bash
# 1. 构建二进制
make build

# 2. 初始化测试网络
make init

# 3. 启动节点
make start

# 4. 查看状态
make status
```

## 架构

```
hcp-consensus/
├── cmd/hcpd/          # 主程序入口
├── app/               # 应用逻辑
├── consensus/         # tPBFT 共识实现
├── scripts/           # 辅助脚本
├── testnet/           # 测试网络数据
└── docker-compose.yml # Docker 部署配置
```

## 性能指标

- **目标 TPS**: 0-25k
- **目标延迟**: <500ms (P99 < 1s)
- **节点规模**: 4-7个验证者

## 技术栈

- **共识**: CometBFT (BFT)
- **应用层**: Cosmos-SDK
- **语言**: Go 1.22+
- **容器化**: Docker & Docker Compose

## License

Apache-2.0
