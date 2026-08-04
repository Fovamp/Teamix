<p align="center">
  <strong>Teamix Cloud</strong>
</p>

<p align="center">
  <strong>云端 AI 协作开发平台</strong>
</p>

> 基于 Reasonix 改造的多用户 AI 协作开发平台。
> 开发者用自然语言描述需求，AI 完成代码——无需本地环境，无需 IDE。

## 环境依赖（需先下载安装）

| 依赖 | 版本 | 用途 | 安装方式 |
|---|---|---|---|
| Go | 1.23+ | 编译后端 | 官网下载 <https://go.dev/dl/> |
| Node.js | 18+ | 前端构建 | 官网下载 <https://nodejs.org/> |
| Python | 3.10+（推荐 3.13） | headroom 运行环境 | 官网下载 <https://python.org/> |
| pnpm | 9+ | 前端包管理 | `npm i -g pnpm` |
| headroom-ai | 最新 | LLM 上下文压缩 | `pip install "headroom-ai[proxy]"` |
| Wails | 最新（可选） | 仅构建桌面客户端 desktop/ | `go install github.com/wailsapp/wails/v2/cmd/wails@latest` |

> Wails 仅在需要打包桌面客户端时安装，Web 部署不需要。

## 快速开始

```bash
# 0. 启动 headroom
headroom proxy --port 8788

# 1. 构建前端
cd web && pnpm install && pnpm build
cd ..

# 2. 编译后端
go build -o teamix.exe ./cmd/reasonix/

# 3. 启动
teamix.exe serve --project C:/path/to/project --addr :8787
```

浏览器打开 <http://localhost:8787>，输入昵称即可使用。

## 技术栈

- 后端: Go 1.23+，单二进制部署（前端 go:embed）
- 前端: Vue3 + Vite
- AI: DeepSeek（模型仅公共配置，公司统一 token）
- 上下文压缩: Headroom（headroomlabs-ai/headroom，官方 Python 版）
- 桌面客户端（可选）: Wails + React（`desktop/`）
