# Teamix 目录结构定稿

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
