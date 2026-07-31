// 纯函数工具：从 ChatArea.vue 拆出，无任何组件状态依赖。

/** 移除 system prompt 注入的标记块（reasoning-language / capability-route / Begin..End） */
export function stripSystemTags(text: string): string {
  if (!text) return text
  return text.replace(/<reasoning-language>[\s\S]*?<\/reasoning-language>/g, '')
    .replace(/<capability-route[\s\S]*?<\/capability-route>/g, '')
    .replace(/--- Begin \[.*?\][\s\S]*?--- End \[.*?\] ---/g, '')
    .trim()
}

/** 创建元素（保留原 el 语义） */
export function el(tag: string, cls?: string, text?: string): HTMLElement {
  const e = document.createElement(tag)
  if (cls) e.className = cls
  if (text) e.textContent = text
  return e
}

export function escHtml(s: any): string {
  return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&#39;')
}

export function fmtTok(n: number): string {
  return n >= 1000 ? (n / 1000).toFixed(1).replace(/\.0$/, '') + 'k' : String(n)
}

export function fmtMoney(n: number, c?: string): string {
  if (typeof n !== 'number' || !isFinite(n)) return '—'
  const s = String(c || '¥').trim()
  const sym = /^(cny|rmb|yuan)$/i.test(s) ? '¥' : /^(usd|dollar)$/i.test(s) ? '$' : s || '¥'
  return sym + (n < 1 ? n.toFixed(4) : n.toFixed(2))
}

export function fmtElapsed(ms: number): string {
  const s = Math.floor(ms / 1000)
  return s < 60 ? s + 's' : Math.floor(s / 60) + 'm ' + s % 60 + 's'
}

export function compactText(s: any, max: number): string {
  const text = String(s || '').replace(/\s+/g, ' ').trim()
  return text.length > max ? text.slice(0, max - 1) + '…' : text
}

export function lineCount(s: any): number {
  const text = String(s || '')
  return text ? text.split(/\r\n|\r|\n/).length : 0
}

/** 从 tool args 提取一句话摘要（展示用） */
export function toolArgsSummary(args: any): string {
  const raw = String(args || '').trim()
  if (!raw) return ''
  try {
    const parsed = JSON.parse(raw)
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      const key = ['cmd', 'command', 'path', 'file', 'query', 'q', 'url', 'prompt', 'input'].find(k => parsed[k])
      if (key) return compactText(parsed[key], 90)
    }
  } catch { /* ignore */ }
  return compactText(raw, 90)
}

/** bash 命令是否属于危险操作（展示拦截用） */
export function dangerousBashCommand(command: string): boolean {
  return /^rm\s+-[^\s]*[rf][^\s]*\b/.test(command)
    || /^git\s+push\b.*\s--force\b/.test(command)
    || /^git\s+push\b.*\s-f\b/.test(command)
    || /^git\s+reset\s+--hard\b/.test(command)
    || /^git\s+clean\s+-f\b/.test(command)
    || /^chmod\s+(?:-R\s+)?777\b/.test(command)
    || /^chown\b/.test(command)
    || /^sudo\b/.test(command)
    || /^mkfs\b/.test(command)
    || /^dd\s+if=/.test(command)
    || /^fdisk\b/.test(command)
}

/** 从 subject 提取"命令 参数:..."前缀（工具卡片展示用） */
export function bashCommandPrefix(subject: string): string {
  const command = String(subject || '').trim()
  if (!command || command.includes('`') || command.includes('$(') || /[;|&<>\n]/.test(command)) return ''
  const fields = command.split(/\s+/).filter(Boolean)
  if (fields.length < 2) return ''
  if (dangerousBashCommand(command)) return ''
  const base = fields[0].toLowerCase()
  if (['npm', 'pnpm', 'yarn', 'bun'].includes(base) && fields[1] && fields[1].toLowerCase() === 'run') return fields.length >= 3 ? fields[0] + ' ' + fields[1] + ' ' + fields[2] + ':*' : ''
  return fields[0] + ' ' + fields[1] + ':*'
}
