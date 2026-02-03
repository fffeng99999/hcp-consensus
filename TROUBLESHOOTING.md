# 🔧 HCP-Consensus 故障排除指南

## 目录

1. [Docker 相关问题](#docker-相关问题)
2. [节点启动问题](#节点启动问题)
3. [网络连接问题](#网络连接问题)
4. [共识问题](#共识问题)
5. [性能问题](#性能问题)

---

## Docker 相关问题

### 问题: 端口已被占用

**错误信息:**
```
Error starting userland proxy: listen tcp4 0.0.0.0:26657: bind: address already in use
```

**诊断:**
```bash
# 查找占用端口的进程
lsof -i :26657
netstat -tuln | grep 26657
```

**解决方案:**

1. **停止冲突服务**
```bash
make stop
docker-compose down
```

2. **强制清理**
```bash
docker-compose down -v
docker system prune -f
```

3. **修改端口**
编辑 `docker-compose.yml`,修改端口映射

---

### 问题: Docker 内存不足

**错误信息:**
```
OOMKilled
```

**解决方案:**

1. **增加 Docker 内存**
   - Docker Desktop: Settings -> Resources -> Memory: 8GB+

2. **临时减少节点数量**
```bash
# 编辑 Makefile
NODE_COUNT := 2  # 从 4 改为 2

make reset
make init
make start
```

---

## 节点启动问题

### 问题: 初始化失败

**错误信息:**
```
Error: failed to initialize node
```

**解决方案:**

1. **检查二进制是否存在**
```bash
ls -lh build/hcpd
# 如果不存在
make build
```

2. **清理旧数据**
```bash
make clean-all
make build
make init
```

3. **检查权限**
```bash
chmod +x build/hcpd
chmod +x scripts/*.sh
```

---

### 问题: 节点不断重启

**症状:**
```bash
docker-compose ps
# 显示 Restarting
```

**诊断:**
```bash
make logs-node0
# 查看错误日志
```

**常见原因:**

1. **初始化数据损坏**
```bash
make stop
make reset
make init
make start
```

2. **配置文件错误**
```bash
# 检查 genesis.json
cat testnet/node0/config/genesis.json | jq

# 检查 config.toml
cat testnet/node0/config/config.toml | grep timeout
```

---

## 网络连接问题

### 问题: 节点无法互联

**症状:**
```bash
curl -s http://localhost:26657/net_info | jq '.result.n_peers'
# 返回 "0"
```

**诊断:**
```bash
# 检查 Docker 网络
docker network ls
docker network inspect hcp-consensus_hcp-network
```

**解决方案:**

1. **重启网络**
```bash
make stop
docker network prune
make start
```

2. **检查 persistent_peers 配置**
```bash
cat testnet/node0/config/config.toml | grep persistent_peers
```

---

### 问题: RPC 端点无法访问

**错误信息:**
```
curl: (7) Failed to connect to localhost port 26657
```

**解决方案:**

1. **检查容器状态**
```bash
docker-compose ps
# 确保状态为 Up
```

2. **检查端口映射**
```bash
docker port hcp-node0
```

3. **等待启动完成**
```bash
sleep 15
make status
```

---

## 共识问题

### 问题: 节点未同步

**症状:**
```json
{
  "catching_up": true
}
```

**解决方案:**

1. **等待同步完成**
```bash
# 每 5 秒检查一次
watch -n 5 'curl -s http://localhost:26657/status | jq .result.sync_info.catching_up'
```

2. **检查区块高度**
```bash
curl -s http://localhost:26657/status | jq '.result.sync_info.latest_block_height'
curl -s http://localhost:26667/status | jq '.result.sync_info.latest_block_height'
# 对比不同节点的高度
```

3. **重启落后节点**
```bash
docker-compose restart node1
```

---

### 问题: 共识停滞

**症状:**
```bash
# 区块高度不增长
curl -s http://localhost:26657/status | jq '.result.sync_info.latest_block_height'
# 返回值不变
```

**诊断:**
```bash
# 查看共识状态
make consensus

# 查看节点日志
make logs | grep -i error
```

**解决方案:**

1. **重启所有节点**
```bash
make restart
```

2. **检查验证者集**
```bash
curl -s http://localhost:26657/validators | jq '.result.validators | length'
# 应该返回 4
```

---

## 性能问题

### 问题: TPS 过低

**症状:**
```bash
make benchmark
# TPS < 20
```

**优化方案:**

1. **调整超时参数**
编辑 `testnet/node0/config/config.toml`:
```toml
[consensus]
timeout_propose = "500ms"  # 降低
timeout_commit = "300ms"   # 降低
```

2. **增加内存池大小**
```toml
[mempool]
size = 20000  # 增加
```

3. **启用空块快速生产**
```toml
create_empty_blocks = true
create_empty_blocks_interval = "0s"
```

4. **重启节点应用配置**
```bash
make restart
```

---

### 问题: 延迟过高

**症状:**
```
P99 Latency > 1000ms
```

**诊断:**
```bash
# 检查网络延迟
docker exec hcp-node0 ping node1

# 检查 CPU 负载
docker stats
```

**优化方案:**

1. **使用 host 网络模式** (仅测试)
2. **减少节点数量**
3. **关闭详细日志**
```toml
log_level = "error"  # 从 info 改为 error
```

---

## 调试工具

### 查看实时日志
```bash
make logs -f
```

### 进入容器
```bash
docker exec -it hcp-node0 sh
```

### 检查内存使用
```bash
docker stats --no-stream
```

### 检查磁盘使用
```bash
du -sh testnet/node*/
```

---

## 完全重置

如果所有方法都失败,尝试完全重置:

```bash
# 1. 停止所有服务
make stop

# 2. 删除所有数据
make clean-all

# 3. 清理 Docker
docker system prune -af
docker volume prune -f

# 4. 重新构建
make build

# 5. 重新初始化
make init

# 6. 重新启动
make start

# 7. 验证
sleep 15
make status
```

---

## 获取帮助

如果仍然无法解决:

1. 收集日志: `make logs > debug.log`
2. 提交 Issue: https://github.com/fffeng99999/hcp-consensus/issues
3. 包含以下信息:
   - 操作系统版本
   - Docker 版本
   - Go 版本
   - 错误日志
