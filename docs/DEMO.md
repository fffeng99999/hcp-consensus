# HCP-Consensus 演示脚本

## 演示流程 (10分钟)

### 第一部分: 系统架构 (2分钟)
展示 PPT 技术路线图

### 第二部分: 节点部署 (3分钟)
```bash
# 查看运行状态
docker-compose ps

# 查看节点同步
curl -s http://localhost:26657/status | jq
```

### 第三部分: 交易测试 (2分钟)
```bash
# 发送测试交易
./build/hcpd tx bank send validator0 \
  hcp1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq0z0z0z \
  1000stake --from validator0 \
  --chain-id hcp-testnet \
  --home ./testnet/node0 \
  --keyring-backend test --yes
```

### 第四部分: 性能测试 (3分钟)
```bash
make benchmark
```

## 预期结果

- **P99延迟**: <500ms ✅
- **TPS**: 50-100 (演示环境)
- **成功率**: >95%
