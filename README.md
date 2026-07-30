<p align="center">
  <strong>Teamix Cloud</strong>
</p>

<p align="center">
  <strong>云端 AI 协作开发平台</strong>
</p>

> 基于 Reasonix 改造的多用户 AI 协作开发平台。
> 开发者用自然语言描述需求，AI 完成代码——无需本地环境，无需 IDE。

## 快速开始

```bash
go build -o teamix.exe ./cmd/reasonix/
teamix.exe serve --teamix --addr :8787
```

浏览器打开 <http://localhost:8787>，输入昵称即可使用。

## 技术栈

- 后端: Go 1.23+，单二进制部署
- 前端: Vue3
- AI: DeepSeek
