# 🚀 HCP-Consensus 快速启动指南

## 一、环境检查 (1分钟)

### 1. 检查工具版本

```bash
# 检查 Go 版本
go version
# 需要: go1.22 或更高

# 检查 Docker
docker --version
# 需要: 20.10+

# 检查 Docker Compose
docker-compose --version
# 需要: 2.0+

# 检查 make
make --version
```

### 2. 检查端口是否可用

```bash
# 确保这些端口未被占用
lsof -i :26657  # 应该为空
lsof -i :26667
lsof -i :26677
lsof -i :26687
```

---

## 二、快速部署 (3分钟)

### 步骤 1: 克隆项目

```bash
git clone https://github.com/fffeng99999/hcap-consensus.git
cd hcap-consensus
```

### 步骤 2: 构建二进制

```bash
make build
```

**预期输出:**
```
✅ Build complete: build/hcapd
```

### 步骤 3: 初始化测试网络

```bash
make init
```

**预期输出:**
```
Initializing 4-node testnet...
✅ Node 0 initialized
✅ Node 1 initialized
✅ Node 2 initialized
✅ Node 3 initialized
✅ Testnet initialization complete!
```

### 步骤 4: 启动节点

```bash
make start
```

**预期输出:**
```
Starting HCP testnet nodes...
✅ Nodes started!

RPC Endpoints:
  Node 0: http://localhost:26657
  Node 1: http://localhost:26667
  Node 2: http://localhost:26677
  Node 3: http://localhost:26687
```

### 步骤 5: 等待节点启动

```bash
# 等待 10 秒让节点完全启动
sleep 10
```

---

## 三、验证部署 (1分钟)

### 1. 检查节点状态

```bash
make status
```

**正常输出示例:**
```json
{
  "latest_block_height": "42",
  "latest_block_time": "2026-02-03T21:30:00Z",
  "catching_up": false
}
```

✅ **关键指标:**
- `latest_block_height` 持续增长
- `catching_up` 为 `false`

### 2. 检查 Docker 容器

```bash
docker-compose ps
```

**预期输出:**
```
NAME          STATE
hcap-node0     Up
hcap-node1     Up
hcap-node2     Up
hcap-node3     Up
```

### 3. 查看网络连接

```bash
curl -s http://localhost:26657/net_info | jq '.result.n_peers'
```

**应该返回:** `"3"` (连接其他 3 个节点)

---

## 四、基础操作

### 发送测试交易

```bash
./build/hcapd tx bank send validator0 \
  hcap1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq0z0z0z \
  1000stake \
  --from validator0 \
  --chain-id hcap-testnet \
  --home ./testnet/node0 \
  --keyring-backend test \
  --yes
```

### 查看节点日志

```bash
# 查看所有节点日志
make logs

# 查看单个节点
make logs-node0
```

### 运行性能测试

```bash
make benchmark
```

**预期结果:**
```
Transaction Stats:
  Success Rate:  98%

Latency:
  Average:       290ms
  P99:           490ms

Throughput:
  TPS:           ~65 tx/s
```

---

## 五、常见问题

### 问题 1: 端口已被占用

**错误信息:**
```
Error: port 26657 already in use
```

**解决方法:**
```bash
# 停止所有容器
make stop

# 或者强制清理
docker-compose down -v
```

### 问题 2: 节点未同步

**症状:**
```json
{
  "catching_up": true
}
```

**解决方法:**
```bash
# 等待 30 秒
sleep 30
make status

# 如果仍然未同步,重启
make restart
```

### 问题 3: 构建失败

**错误信息:**
```
go: module not found
```

**解决方法:**
```bash
# 下载依赖
go mod download
go mod tidy

# 重新构建
make build
```

### 问题 4: Docker 内存不足

**解决方法:**
1. 打开 Docker Desktop
2. Settings -> Resources -> Memory
3. 设置为 **8GB** 或更高
4. 重启 Docker

---

## 六、停止和清理

### 停止节点

```bash
make stop
```

### 清理所有数据

```bash
make clean-all
```

**注意:** 这会删除所有测试网络数据!

---

## 七、下一步

### 阅读详细文档

- [README.md](README.md) - 项目概览
- [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md) - 详细部署指南
- [docs/DEMO.md](docs/DEMO.md) - 演示脚本

### 进阶操作

```bash
# 查看所有命令
make help

# 运行对比实验
bash scripts/compare-consensus.sh

# 查看 API 文档
curl http://localhost:26657/
```

---

## ✅ 成功标志

如果你看到以下输出,说明部署成功:

```bash
make status
```

```json
{
  "node_info": {
    "network": "hcap-testnet"
  },
  "sync_info": {
    "latest_block_height": "100",
    "catching_up": false
  },
  "validator_info": {
    "voting_power": "100000000"
  }
}
```

✅ **恭喜!你已经成功部署 HCP-Consensus 区块链节点!**

---

## 📞 获取帮助

如果遇到问题:

1. 查阅 [README.md](README.md) 常见问题部分
2. 查看节点日志: `make logs`
3. 提交 Issue: https://github.com/fffeng99999/hcap-consensus/issues
