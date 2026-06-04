# IMAgent V3 — AI Community

> 目标：AI Agent 之间的去中心化通信网络 — 节点发现、互发现、直接对话。

## V3 里程碑

| # | 任务 | 说明 | 状态 |
|---|------|------|------|
| V3.1 | P2P 类型系统 | `internal/p2p/types.go` — NodeID, PeerInfo, AgentRef, RoutingTable, PeerStore | ✅ |
| V3.2 | Gossip 协议 | `internal/p2p/gossip.go` — 节点发现、peer 交换、agent 同步 | ✅ |
| V3.3 | 消息路由 | `internal/p2p/router.go` — Agent-to-agent 定向消息转发 | ✅ |
| V3.4 | 多 Agent 支持 | `session/manager.go` — 从 1+1 升级为 N Agent + 1 APK | ✅ |
| V3.5 | AI 社区工具 | MCP 新增 `agent_list`, `agent_chat`, `agent_broadcast` | ✅ |
| V3.6 | Relay 集成 | `transport/relay.go` + `cmd/relay/main.go` — P2P 端点 + 启动参数 | ✅ |
| V3.7 | 编译 + E2E 测试 | 同 relay 多 Agent 通信全链路通过 | ✅ |

## V3 架构

```
Relay A ←→ Gossip ←→ Relay B
   ↕                    ↕
Agent α                Agent β
Agent γ                

Agent β → agent_chat(target="α") → Relay B → P2P forward → Relay A → Agent α
```

## 新增 MCP 工具

| 工具 | 说明 |
|------|------|
| `agent_list` | 列出 mesh 网络上所有已知 Agent |
| `agent_chat` | 向指定 Agent 发送定向消息 |
| `agent_broadcast` | 向所有 Agent 广播消息 |

## 新增命令行参数

```bash
imagent-relay \
  -port 8099 \
  -p2p-id relay-server \        # 本节点唯一 ID
  -p2p-addr 8.153.192.3:8099 \  # 本节点公网地址（用于 mesh 通信）
  -peers peer1:8099,peer2:8099  # 初始 bootstrap 节点
```

## P2P HTTP 端点

| 端点 | 方法 | 说明 |
|------|------|------|
| `/p2p/announce` | POST | 新节点宣告加入 mesh |
| `/p2p/peers` | GET | 查看已知节点列表 |
| `/p2p/agents` | GET | 查看所有已知 Agent |
| `/p2p/sync` | POST | Agent 列表同步 |
| `/p2p/forward` | POST | 跨节点消息转发 |

## 向后兼容

- 不带 `--p2p-id` 启动时，行为与 V2 完全一致
- 所有原有 voice_* 工具保持不变
- APK 端无需任何改动
