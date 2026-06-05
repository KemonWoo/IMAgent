# IMAgent V4 — Self-Evolution

> 目标：Relay 具备自我感知、自我修复、自我更新能力。

## V4 里程碑

| # | 任务 | 说明 | 状态 |
|---|------|------|------|
| V4.1 | Metrics | Prometheus `/metrics` 端点，Gauge/Counter 注册表 | ✅ |
| V4.2 | 自适应重连 | 指数退避 + 熔断器，P2P bootstrap 自动恢复 | ✅ |
| V4.3 | 自愈 | 30s 周期健康检查、内存>500MB 自动 GC | ✅ |
| V4.4 | 更新检查 | `/version` + `/update/check` (GitHub Release API) | ✅ |

## 新增端点

| 端点 | 说明 |
|------|------|
| `GET /metrics` | Prometheus 格式指标（goroutines, memory, connections, messages） |
| `GET /version` | 版本号、Go 版本、OS/Arch |
| `GET /update/check` | 查询 GitHub 最新 release，对比当前版本 |

## Metrics 指标

| 指标 | 类型 | 说明 |
|------|------|------|
| `go_goroutines` | gauge | 当前 goroutine 数 |
| `go_mem_alloc_bytes` | gauge | 堆内存分配 |
| `relay_connections` | gauge | 当前活跃连接数 |
| `relay_messages_total` | counter | 累计消息数 |
| `relay_errors_total` | counter | 累计错误数 |
| `relay_uptime_seconds` | gauge | 进程运行时间 |

## 自愈策略

| 场景 | 动作 |
|------|------|
| 内存 > 500MB | 强制 GC |
| P2P 节点连接失败 | 指数退避重试（1s→2s→4s→...→5min） |
| P2P 节点连续 5 次失败 | 熔断器打开，300s 冷却 |
| 所有连接断开 | 静默等待（正常 idle） |
