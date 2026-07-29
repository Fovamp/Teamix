# Teamix Cloud 改造计划

> 基于 Reasonix 的多用户 AI 协作开发平台 — 架构改造总纲
> 创建日期：2026-07-28

---

## 执行顺序

| 顺序 | 任务 | 类型 | 依赖 |
|:---:|------|------|------|
| 1 | 仓库剥离 | 仓库整理 | 无 |
| 2 | 后端 handler 拆分 | 架构改造 | 无 |
| 3 | Changelog 系统 | 功能新增 | 无 |
| 4 | Headroom 压缩 | 成本优化 | 无 |
| 5 | 前端 Vue3 改造 | 架构改造 | 依赖后端 API 稳定（任务 2） |
| 6 | 双模型协作 | 架构改造 | 依赖框架层稳定（任务 2） |
| 7 | 架构师 Web 自维护 | 架构改造 | 依赖 Vue3 改造（任务 5） |
| 8 | K8s 部署架构 | 部署架构 | 依赖前面功能跑通 |
| 9 | 会话隔离 | 存储改造 | 无 |

---

## 1. 仓库剥离 ✅ 已完成

**问题：** Fork 中包含大量多余的上游目录（benchmarks/docs/site/workers/tools/npm/scripts 等），影响开发体验。

**方案：** 一次 git rm 删除不需要的目录和文件，保持 Fork 关系，后续仍可合并上游核心更新。

---

## 2. 后端 handler 拆分

**问题：** internal/serve/teamix.go 1200+ 行，Handler 与业务逻辑混合，错误响应格式不统一，配置中心 API 直接读 YAML。

**方案：**
- 按领域拆分为 teamix_auth.go / teamix_workflow.go / teamix_config.go
- 统一错误响应格式
- 配置中心 API 改为调 Controller 方法
- 建立标准中间件链（日志/CORS/限流）
- 不引入 Gin/Echo 框架，原生 ServeMux 够用

**配置统一：** MCP/Skills/密钥池是 Agent 基础设施，不需要隔离。统一配置路径，Desktop 和 Web 配置中心都能操作。

---

## 3. AI 变更追踪系统（Changelog）

**问题：** AI 每次改代码不可追溯，无法逐条审查/接受/拒绝，跨会话无法追溯需求来源。

**方案：**
- 独立于 git 的 Changelog 系统
- 拦截 write_file/edit_file 的 SSE 事件 → 自动生成 diff
- 持久化到 .teamix/changelogs/
- 前端变更面板展示 diff 高亮 + 逐条 [✓接受] [✗拒绝]
- 拒绝时从备份文件反向恢复
- 不修改 Agent 工具链，只在 Teamix 层面监听事件流

---

## 4. Headroom 上下文压缩

**问题：** 内网模型只有 6 万上下文窗口，JSON 工具结果动辄 50K 填满窗口。

**方案：**
- 部署 Headroom 透明 Proxy（:8788），Teamix 改 LLM endpoint 指向它
- SmartCrusher 压 JSON 60-95%，CodeCompressor 压代码 15-20%
- 纯算法零 AI 调用开销
- 关闭自然语言压缩，保持 DeepSeek 前缀缓存命中率
- 与 Reasonix compact.go 分工：Headroom 每轮压内容体积，compact.go 窗口快满时压对话轮数

---

## 5. 前端 Vue3 改造

**问题：** index.html 3483 行 220KB 单文件，无组件化、无构建工具、无状态管理。

**方案：**
- 选 Vue3 理由：团队前端技术栈就是 Vue3
- 保留单二进制部署：npm run build → //go:embed dist
- 两步迁移：
  - 共存期：新增 teamix-web/ Vue3 项目，/teamix-v3/* 路由共存，先用 Vue3 重写对话+工作流+登录
  - 逐步替换：功能逐一迁移，覆盖全部后移除 index.html

---

## 6. 内/外网双模型协作

**问题：** 内网 Qwen 模型推理弱但能接触代码/私密数据，外网 DeepSeek 推理强但不能出内网。

**方案：**
- 分界线：是否需要访问文件系统？需要→内网，不需要→外网
- **外网模型 = 设计师/技术主管**：工具集受限（无 bash/read_file），产出 PRD/方案/SQL
- **内网模型 = 执行者/开发工程师**：全工具集，写代码跑测试，卡住时调 call_external
- 双 Controller 架构：localCtrl + cloudCtrl，默认 activeCtrl = localCtrl（内网优先）
- 最终代码质量 = 外网的方案质量 × 内网的执行质量 × Agent 工具循环的纠错能力

---

## 7. 架构师 Web 自维护（替代 Desktop）

**问题：** Linux 服务器无桌面环境无法运行 Desktop 客户端，但架构师需要维护 Teamix 自身配置和代码。

**方案：**
- 架构师登录 Teamix Web 时，工作区指向 Teamix 自身源码
- AI Agent 直接修改 Teamix 的工作流/配置/UI/后端代码
- 改完重启生效，不需在 Linux 上装桌面环境
- Desktop 保留作为本地可选工具

---

## 8. K8s 部署架构

**问题：** 每人一个容器跑全套项目浪费资源（15 人 × 4GB = 60GB）。

**方案：**
- 共享 Namespace：MySQL/Redis/公共模块一直运行
- 每人独立 Namespace：前端 + 个人负责的模块，不活跃自动回收
- workspaceRoot 从 TeamixServer 下放到 userSession
- 增量编译 mvn compile -pl + Actuator restart 替代全量 clean install
- 验证策略分层：前端 0.5 秒热更新 → 单元测试 5 秒 → Actuator 重启 30 秒

---

## 9. 会话隔离

**问题：** Teamix 会话记录存在 Reasonix 共享目录，与上游耦合。

**方案：**
- 将 Teamix 会话目录改为项目工作区内的 .teamix/sessions/
- 每个项目独立携带会话记录
- 迁移服务器时一并带走
- 与 Reasonix 彻底解耦
