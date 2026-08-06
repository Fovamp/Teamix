# Teamix 目录结构定稿

> 全量运行数据目录结构，git 维护，供后续开发者参考。
> 原则：先类型后项目、全局/用户对称、项目克隆纯净、实体仓库与引擎入口分离。

## 全量结构

```
GlobalProject/                                ← 团队工作区根
│
├── .reasonix/                                ← 团队级实体仓库（技能/MCP/人格/密钥的实际文件）
│   ├── secrets/                              ← 密钥池（pool.yaml）
│   ├── skills/                               ← 团队技能【实体】
│   ├── mcp.json                              ← 团队 MCP【实体】
│   └── soul.yaml                             ← 团队人格库【实体】（架构师维护）
│
├── .teamix/                                  ← 团队级协作配置 + 团队数据
│   ├── config.yaml                           ← models/sensitive/audit/quota/alert
│   ├── projects.yaml                         ← 项目清单
│   ├── users.yaml                            ← 用户白名单 + 角色 + allow_external
│   ├── memory/<项目名>/                      ← 团队全局记忆（架构师维护，全员 agent 只读）
│   ├── workflows/                            ← 团队工作流模板
│   ├── notifications/                        ← 通知配置
│   └── logs/ai-audit/<用户>/<日期>.jsonl     ← AI 审计日志（仅架构师）
│
├── projects/<项目名>/                         ← 公共区克隆（团队共享副本）
│
└── users/
    └── <用户名>/
        ├── .reasonix/                        ← 用户级实体仓库
        │   ├── skills/                       ← 私有技能【实体】
        │   ├── mcp-private.json              ← 私有 MCP【实体】
        │   ├── soul.yaml                     ← 私有人格库【实体】
        │   └── persona-current.md            ← 当前生效人格（引擎读取）
        │
        ├── .teamix/                          ← 用户级协作配置 + 运行数据
        │   ├── config.yaml                   ← git 凭证 + 偏好（仅此两项）
        │   ├── workflows/                    ← 用户工作流模板
        │   ├── memory/<项目名>/              ← 项目记忆
        │   │   ├── private/                  ← 私有记忆（agent 手动 remember）
        │   │   └── compiler/                 ← Memory v5 编译状态（AI 自动整理记忆，默认启用）
        │   ├── sessions/<项目名>/            ← 该项目会话
        │   ├── summaries/<项目名>/           ← 该项目总结
        │   └── tmp/                          ← 未选项目的临时数据（隔离不互通）
        │       ├── sessions/                 ← 临时会话
        │       └── memory/
        │           ├── private/              ← 临时手动记忆
        │           ├── global/               ← 临时全局记忆
        │           └── compiler/             ← 临时 AI 自动记忆
        │
        ├── <项目名>/                          ← 用户的项目克隆（git 仓库，纯净无 .teamix）
        └── reasonix.toml                     ← 引擎入口：只写路径，不存内容
            [skills] paths          → .reasonix/skills/
            [[plugins]]             → .reasonix/mcp.json + mcp-private.json
            [agent] system_prompt_file → .reasonix/persona-current.md
```

## 三层职责（核心约定）

| 层 | 职责 | 内容 |
|---|---|---|
| **`.reasonix/`** | 实体仓库（存东西） | 技能、MCP、人格库、当前人格、密钥的实际文件 |
| **`reasonix.toml`** | 引擎入口（指路径） | 技能路径、MCP 插件、人格文件路径——**不存内容** |
| **`.teamix/`** | 协作 + 运行数据 | 凭证/偏好（config.yaml）、记忆/会话/总结/审计/工作流/临时 |

## 关键设计决策

1. **先类型后项目**：`memory/`、`sessions/`、`summaries/` 并列，项目在下一层——一眼看出"这是什么 → 归属哪个项目"；全局与用户完全对称
2. **项目克隆纯净**：`users/<名字>/<项目名>/` 只有 git 代码，**零 .teamix 内容**（运行数据全在用户 .teamix 内）
3. **全局记忆一份**：只在 `GlobalProject/.teamix/memory/<项目>/`（架构师 UI 维护），agent 只读引用（`ReadOnlyGlobal`，非架构师不可写）
4. **技能/MCP/人格实体统一在 `.reasonix/`**，reasonix.toml 只写路径（一致风格）；人格本体存 soul.yaml（人格库可存多个），persona-current.md 是当前生效那份
5. **`.teamix/config.yaml` 只保留协作字段**（git 凭证 + preferences）；MCP/Skills 死字段已删（实体在 .reasonix/）
6. **未选项目进 tmp**：`tmp/{sessions,memory/}` 存临时数据，与项目隔离不互通；总结强制要求先选项目（不进 tmp）
7. **compiler（AI 自动整理记忆）默认启用**：每次对话完成任务自动提炼结构化记忆，落 `memory/<项目>/compiler/`（懒创建，首次写入才建目录）
8. **审计日志**：`GlobalProject/.teamix/logs/ai-audit/<用户>/`（仅架构师可见）

## 各数据隔离维度

| 数据 | 位置 | 隔离 |
|---|---|---|
| 私有记忆 | `users/<名字>/.teamix/memory/<项目>/private/` | 用户 × 项目 |
| 全局记忆 | `GlobalProject/.teamix/memory/<项目>/` | 项目（团队一份，只读） |
| 会话 | `users/<名字>/.teamix/sessions/<项目>/` | 用户 × 项目 |
| 总结 | `users/<名字>/.teamix/summaries/<项目>/` | 用户 × 项目 |
| AI 自动记忆 | `users/<名字>/.teamix/memory/<项目>/compiler/` | 用户 × 项目 |
| 临时数据 | `users/<名字>/.teamix/tmp/` | 用户（未选项目，不互通） |
| 审计日志 | `GlobalProject/.teamix/logs/ai-audit/<用户>/` | 用户（架构师可见） |
| 人格/技能/MCP | `.reasonix/`（全局）+ 用户 `.reasonix/` | 不按项目 |

## 注意

- `memory/<项目>/private/` 保留（不拍平）：与 `compiler/` 并列，语义清晰（private=手动记忆、compiler=AI 自动记忆）
- `webdist-v3` 是 go:embed 编译依赖（构建产物，gitignored，不可删）
- 密钥（QWEN_API_KEY 等）只在 `.env`（gitignored），勿入任何配置/文档/仓库
