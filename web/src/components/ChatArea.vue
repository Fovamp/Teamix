<script setup lang="ts">
import { ref, onMounted, onUnmounted, nextTick } from "vue"
import { api } from "../api"

// ── helpers ──
function stripSystemTags(text: string): string {
  if (!text) return text
  // Remove <reasoning-language>...</reasoning-language> and <capability-route>...</capability-route> blocks
  return text.replace(/<reasoning-language>[\s\S]*?<\/reasoning-language>/g, '')
    .replace(/<capability-route[\s\S]*?<\/capability-route>/g, '')
    .replace(/--- Begin \[.*?\][\s\S]*?--- End \[.*?\] ---/g, '')
    .trim()
}

function el(tag: string, cls?: string, text?: string) {
  const e = document.createElement(tag)
  if (cls) e.className = cls
  if (text) e.textContent = text
  return e
}
function escHtml(s: any) { return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&#39;') }
function fmtTok(n: number) { return n >= 1000 ? (n / 1000).toFixed(1).replace(/\.0$/, '') + 'k' : String(n) }
function bashCommandPrefix(subject: string): string {
  const command = String(subject || '').trim();
  if (!command || command.includes('`') || command.includes('$(') || /[;|&<>\n]/.test(command)) return '';
  const fields = command.split(/\s+/).filter(Boolean);
  if (fields.length < 2) return '';
  if (dangerousBashCommand(command)) return '';
  const base = fields[0].toLowerCase();
  if (['npm','pnpm','yarn','bun'].includes(base) && fields[1] && fields[1].toLowerCase() === 'run') return fields.length >= 3 ? fields[0]+' '+fields[1]+' '+fields[2]+':*' : '';
  return fields[0]+' '+fields[1]+':*';
}
function dangerousBashCommand(command: string): boolean {
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
    || /^fdisk\b/.test(command);
}

function fmtMoney(n: number, c?: string) {
  if (typeof n !== 'number' || !isFinite(n)) return '—'
  const s = String(c || '¥').trim()
  const sym = /^(cny|rmb|yuan)$/i.test(s) ? '¥' : /^(usd|dollar)$/i.test(s) ? '$' : s || '¥'
  return sym + (n < 1 ? n.toFixed(4) : n.toFixed(2))
}
function fmtElapsed(ms: number) {
  const s = Math.floor(ms / 1000)
  return s < 60 ? s + 's' : Math.floor(s / 60) + 'm ' + s % 60 + 's'
}
function compactText(s: any, max: number) {
  const text = String(s || '').replace(/\s+/g, ' ').trim()
  return text.length > max ? text.slice(0, max - 1) + '…' : text
}
function lineCount(s: any) {
  const text = String(s || '')
  return text ? text.split(/\r\n|\r|\n/).length : 0
}
function toolArgsSummary(args: any) {
  const raw = String(args || '').trim()
  if (!raw) return ''
  try {
    const parsed = JSON.parse(raw)
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      const key = ['cmd', 'command', 'path', 'file', 'query', 'q', 'url', 'prompt', 'input'].find(k => parsed[k])
      if (key) return compactText(parsed[key], 90)
    }
  } catch { }
  return compactText(raw, 90)
}

// ── state ──
const messages = ref<any[]>([])
const inputText = ref("")
const pastedBlocks = ref<{ label: string; text: string }[]>([])
const openPastedLabels = ref<string[]>([])
const previewHeight = ref(120)
const composerResized = ref(false)
let composerDefaultHeight = 0
let pasteIdCounter = 1
const running = ref(false)
const statusText = ref("就绪")
const planMode = ref(false)
const bypassMode = ref(false)
const goalMode = ref(false)
const goalActive = ref(false)
const goalText = ref("")
const cwd = ref("-")
const cwdTitle = ref("")
const wfName = ref("-")
const wfStages = ref<any[]>([])
const wfVisible = ref(false)
let activeStageIdx = -1
const statusModel = ref("-")
const showRewind = ref(false)
const showTodoPanel = ref(false)

// cumulative stats
let cumulativeCost = 0
let cumulativeCacheHit = 0
let cumulativeCacheMiss = 0
function resetCumulativeStats() { cumulativeCost = 0; cumulativeCacheHit = 0; cumulativeCacheMiss = 0 }

// rewind state
let rewindCheckpoints: any[] = []
let rewindStage = 0
let rewindSelected = 0
let rewindScope = 0
const rewindKey = ref(0)
const SCOPES = [
  { key: 'b', label: '代码 + 对话', scope: 'both' },
  { key: 'c', label: '仅对话', scope: 'conversation' },
  { key: 'd', label: '仅代码', scope: 'code' },
  { key: 'f', label: '分叉（新分支）', scope: 'fork' },
  { key: 's', label: '从此处总结', scope: 'sumfrom' },
  { key: 'u', label: '总结到此处', scope: 'sumupto' },
]

// tool cards
const toolCards: Record<string, HTMLElement> = {}

// slash commands
const SLASH_CMDS = [
  { cmd: 'new', sig: '/new', desc: '新建会话', group: 'session' },
  { cmd: 'resume', sig: '/resume [n]', desc: '恢复会话', group: 'session' },
  { cmd: 'compact', sig: '/compact', desc: '压缩对话', group: 'session' },
  { cmd: 'rewind', sig: '/rewind', desc: '回退到检查点', group: 'session' },
  { cmd: 'tree', sig: '/tree', desc: '显示分支树', group: 'branch' },
  { cmd: 'branch', sig: '/branch <name>', desc: '创建分支', group: 'branch' },
  { cmd: 'switch', sig: '/switch <id>', desc: '切换分支', group: 'branch' },
  { cmd: 'model', sig: '/model [provider/model]', desc: '列出/切换模型', group: 'model' },
  { cmd: 'effort', sig: '/effort <level>', desc: '推理努力级别', group: 'model' },
  { cmd: 'goal', sig: '/goal <task>', desc: '设置目标让代理自主执行', group: 'agent' },
  { cmd: 'thinking', sig: '/thinking <level>', desc: '思考努力', group: 'agent' },
  { cmd: 'verbose', sig: '/verbose', desc: '切换推理显示', group: 'agent' },
  { cmd: 'mcp', sig: '/mcp', desc: 'MCP 服务器', group: 'system' },
  { cmd: 'skill', sig: '/skill', desc: '技能', group: 'system' },
  { cmd: 'hooks', sig: '/hooks', desc: '钩子', group: 'system' },
  { cmd: 'memory', sig: '/memory', desc: '显示记忆', group: 'memory' },
  { cmd: 'forget', sig: '/forget <item>', desc: '忘记记忆', group: 'memory', danger: true },
  { cmd: 'help', sig: '/help', desc: '帮助', group: 'help' },
]

let slashOpen = ref(false)
let slashIndex = 0
let slashFiltered: any[] = []

// chat history
let chatHistory: string[] = []
let chatHistoryIndex = 0
let firstTurnDone = false
const hasVisibleHistory = ref(false)
let checkpointCount = 0

// approval
let pendingPrompts: Function[] = []
function clearPendingPrompts() { pendingPrompts.forEach(f => f()); pendingPrompts = [] }

// workflow stage
let stageCompletePending = false
let stageCompleteReason = ""
let stageCompleteExtra = ""
let pendingPages: { url: string; label: string }[] = []

// todos
let todosState: any[] = []
let todosDismissed = false

// SSE
let es: EventSource | null = null
let escTimer: ReturnType<typeof setTimeout> | null = null
let tickTimer: ReturnType<typeof setInterval> | null = null
let turnStartAt = 0
let turnTokens = 0
let statusPollTimer: ReturnType<typeof setInterval> | null = null

// ── init ──
onMounted(() => {
  loadHistory()
  connectSSE()
  loadProjectInfo()
  loadWorkflow()
  window.addEventListener("open-rewind-picker", () => { openRewindPicker() })
  window.addEventListener("model-changed", () => { setTimeout(fetchStatus, 500) })
  window.addEventListener("new-session-requested", () => { handleNewSession() })
  window.addEventListener("session-resumed", ((e: CustomEvent) => {
    loadHistory()
    setTimeout(loadWorkflow, 300)
  }) as any)
  window.addEventListener("session-deleted", () => {
    messages.value = []
    hasVisibleHistory.value = false
    finalizeMsg()
    currentMsgEl = null; currentTextEl = null; currentReasoningEl = null
    showWelcome()
    document.querySelectorAll('.card, .approval, .ask, .compaction, .metric-strip, .notice, .phase, .msg--error').forEach(el => el.remove())
    checkpointCount = 0
    todosState = []
    showTodoPanel.value = false
  })
  window.addEventListener("workflow-changed", loadWorkflow)
  window.addEventListener("workflow-selected", ((e: CustomEvent) => {
    const name = e.detail || ""
    wfName.value = name || "-"
    // Persist to localStorage for page refresh
    if (name) { localStorage.setItem('teamix_wf_name', name) }
    else { localStorage.removeItem('teamix_wf_name') }
    const el = document.getElementById('welcome-wf')
    if (el) el.textContent = name || ""
  }) as any)
  statusPollTimer = setInterval(fetchStatus, 30000)
  fetchStatus()
  document.addEventListener('keydown', onGlobalKeydown)

  // Log scroll for auto-scroll
  const log = document.getElementById('log')
  if (log) {
    log.addEventListener('scroll', () => {
      pinnedToBottom = log.scrollHeight - log.scrollTop - log.clientHeight < 40
    })
    log.addEventListener('wheel', (e: WheelEvent) => {
      if (e.deltaY < 0 && log.scrollHeight > log.clientHeight) pinnedToBottom = false
    }, { passive: true })
  }

  // Drag-drop on input
  const inp = document.getElementById('in')
  if (inp) {
    inp.addEventListener('dragover', (e) => e.preventDefault())
    inp.addEventListener('drop', handleFileDrop)
  }
})
onUnmounted(() => {
  if (es) es.close()
  if (statusPollTimer) clearInterval(statusPollTimer)
  if (tickTimer) clearInterval(tickTimer)
  window.removeEventListener("workflow-changed", loadWorkflow)
  document.removeEventListener('keydown', onGlobalKeydown)
})

// ── history load ──
function loadProjectInfo() {
  api.project().then(data => {
    if (data && data.workspaceRoot) {
      const fullPath = data.workspaceRoot.replace(/\\/g, '/')
      let parts = fullPath.split('/').filter(Boolean)
      if (parts.length > 0 && parts[0].endsWith(':')) parts[0] = parts[0].slice(0, -1)
      cwdTitle.value = '/' + parts.join('/')
      if (parts.length <= 2) { cwd.value = '/' + parts.join('/') }
      else { cwd.value = '/' + parts.slice(0, 2).join('/') + '/.../' + parts.slice(-2).join('/') }
    }
  }).catch(() => {})
}

function handleNewSession() {
  console.log('handleNewSession called, hasVisibleHistory:', hasVisibleHistory.value, 'chatHistory.length:', chatHistory.length)
  const hasContent = hasVisibleHistory.value || chatHistory.length > 0
  if (!hasContent) {
    showWelcome()
    messages.value = []
    hasVisibleHistory.value = false
    // Still refresh sessions list to show correct state
    api.sessions().then(ss => {
      window.dispatchEvent(new CustomEvent('sessions-update', { detail: ss }))
    }).catch(() => {})
    return
  }
  const t = localStorage.getItem('teamix_token')
  if (!t) return
  fetch('/new?token=' + encodeURIComponent(t), { method: 'POST', headers: { 'Content-Type': 'application/json' } }).then(() => {
    showWelcome()
    messages.value = []
    hasVisibleHistory.value = false
    chatHistory = []
    chatHistoryIndex = 0
    firstTurnDone = false
    checkpointCount = 0
    todosState = []
    todosDismissed = false
    showTodoPanel.value = false
    resetCumulativeStats(); try { sessionStorage.removeItem('teamix_last_usage') } catch {}; pastedBlocks.value = []; openPastedLabels.value = []
    // Refresh sessions list so sidebar shows new session
    api.sessions().then(ss => {
      window.dispatchEvent(new CustomEvent('sessions-update', { detail: ss }))
    }).catch(() => {})
  }).catch((err) => { console.error('handleNewSession error:', err) })
}

async function loadHistory() {
  try {
    const h = await api.history()
    const ms = (h?.messages || h || [])
    renderHistoryMessages(ms)
    setTimeout(() => scrollDown(true), 100)
  } catch (e) { console.error("loadHistory", e) }
  try {
    const s = await api.status()
    running.value = s.running
    // Expose cumulative stats for StatsModal\n  (window as any)._cumulativeStats = { cost: cumulativeCost, tokens: cumulativeCost > 0 ? cumulativeCost * 1000 : 0, cacheHit: cumulativeCacheHit, cacheMiss: cumulativeCacheMiss }\n  planMode.value = !!s.plan
  } catch (e) { console.error("loadStatus", e) }
}

function renderHistoryMessages(ms: any[]) {
  if (!ms || ms.length === 0) {
    hasVisibleHistory.value = false
    window.dispatchEvent(new CustomEvent('hasVisibleHistory-changed', { detail: false }))
    checkpointCount = 0
    return
  }
  const visible = ms.some((m: any) => {
    if (m.role === 'user') return !!m.content
    if (m.role === 'assistant') return !!(m.content || m.reasoning || (m.toolCalls && m.toolCalls.length))
    if (m.role === 'tool') return !!(m.content || m.toolCallId || m.toolName)
    return false
  })
  hasVisibleHistory.value = visible
  window.dispatchEvent(new CustomEvent('hasVisibleHistory-changed', { detail: visible }))
  if (!visible) return
  hideWelcome()
  messages.value = ms.filter((m: any) => m.role !== 'system').map((m: any) => {
    if (m.content) m.content = stripSystemTags(m.content)
    if (m.reasoning) m.reasoning = stripSystemTags(m.reasoning)
    return m
  })
  Object.keys(toolCards).forEach(k => delete toolCards[k])
  // Restore usage strip from sessionStorage
  try {
    const saved = sessionStorage.getItem('teamix_last_usage')
    if (saved && messages.value.length > 0) {
      const lastMsg = messages.value[messages.value.length - 1]
      if (lastMsg.role === 'assistant' || lastMsg.role === 'user') {
        showUsageStrip(JSON.parse(saved))
      }
    }
  } catch {}
}

// ── SSE ──
function connectSSE() {
  const t = localStorage.getItem("teamix_token")
  if (!t) { console.log("no token for SSE"); return }
  const url = "/events?token=" + encodeURIComponent(t)
  console.log("SSE connecting to", url)
  es = new EventSource(url)
  es.onopen = () => { setConnState('connected'); fetchStatus(); fetchTodos(); loadWorkflow() }
  es.onmessage = (evt) => {
    setConnState('connected')
    try {
      const e = JSON.parse(evt.data)
      if (e.kind !== 'retrying') clearRetrying()
      switch (e.kind) {
        case 'turn_started':
          setRunning(true)
          clearPendingPrompts()
          finalizeMsg()
          Object.keys(toolCards).forEach(k => delete toolCards[k])
          todosDismissed = false
          stageCompleteReason = ''
          stageCompleteExtra = ''
          pendingPages = []
          break
        case 'reasoning':
          appendReasoning(e.reasoning || e.text || '')
          break
        case 'text':
          handleTextSSE(e.text || '')
          break
        case 'message':
          finalizeMsg()
          break
        case 'tool_dispatch':
          if (e.tool) renderToolDispatch(e.tool)
          break
        case 'tool_result':
          if (e.tool) {
            renderToolResult(e.tool)
            if (e.tool.name === 'todo_write' && !e.tool.parentId && !e.tool.err) {
              try {
                const ts = parseTodos(e.tool.args)
                if (ts.length) { todosState = ts; renderTodoPanel() }
              } catch { }
            }
          }
          break
        case 'tool_progress':
          if (e.tool) renderToolProgress(e.tool)
          break
        case 'usage':
          if (e.usage) {
            turnTokens = e.usage.completionTokens || 0
            cumulativeCost += e.usage.cost ?? e.usage.costUsd ?? 0
            cumulativeCacheHit += e.usage.cacheHitTokens || 0
            cumulativeCacheMiss += e.usage.cacheMissTokens || 0
            showUsageStrip(e.usage)
          }
          break
        case 'notice':
          showNotice(e.text || '', e.level === 'warn' ? 'warn' : '')
          break
        case 'phase':
          finalizeMsg()
          showPhase(e.text || '')
          break
        case 'approval_request':
          if (e.approval) showApproval(e.approval)
          break
        case 'ask_request':
          if (e.ask) showAsk(e.ask)
          break
        case 'compaction_started':
          showCompaction({ trigger: e.compaction?.trigger })
          break
        case 'compaction_done':
          showCompaction(e.compaction || {})
          break
        case 'retrying':
          setRetrying(e.retryAttempt, e.retryMax)
          break
        case 'turn_done':
          clearPendingPrompts()
          finalizeMsg()
          setRunning(false)
          if (e.err) { showError(e.err) }
          fetchStatus()
          fetchNotifications()
          fetchTodos()
          refreshCheckpointAvailability()
          loadWorkflow()
          if (!firstTurnDone && !e.err) { firstTurnDone = true; loadSessions() }
          if (pendingPages.length > 0 && !e.err) { showOpenPageCard(pendingPages); pendingPages = [] }
          if (stageCompletePending && !e.err) { showStageApproval(stageCompleteReason) }
          // Fallback: check accumulated text in case marker was split across chunks
          if (!stageCompletePending && !e.err && window._wfLastText && window._wfLastText.indexOf('\u9636\u6bb5\u5b8c\u6210') >= 0) {
            stageCompletePending = true
            try { sessionStorage.setItem('wf_advance_pending', '1') } catch (e) {}
            showStageApproval('')
          }
          window._wfLastText = ''
          break
      }
    } catch (ex) { console.error("SSE parse error", ex, evt.data) }
  }
  es.onerror = () => {
    if (es?.readyState === EventSource.CONNECTING) setConnState('reconnecting')
    else setConnState('disconnected')
  }
}

// ── SSE text handler ──
function handleTextSSE(txt: string) {
  window._wfLastText = (window._wfLastText || "") + txt
  const pgRe = /__打开页面__:\s*(\S+)\s*-\s*(.+)/g
  let m: RegExpExecArray | null
  while ((m = pgRe.exec(txt)) !== null) {
    const url = m[1]
    const label = m[2].trim()
    if (url && label && (url.indexOf('http:') === 0 || url.indexOf('https:') === 0)) {
      const dup = pendingPages.some(p => p.url === url)
      if (!dup) pendingPages.push({ url, label })
    }
  }
  const clean = txt.replace(/__打开页面__:[^\n]*\n?/g, '').replace(/__打开页面__[^:\n]*/g, '').replace(/__JUMP_TO__/g, '').replace(/__STAGE_JUMP__/g, '')
  const idx = clean.indexOf('阶段完成')
  if (idx >= 0) {
    const before = clean.substring(0, idx)
    const after = clean.substring(idx + 4)
    if (after.indexOf(':') === 0) {
      const reason = after.substring(1).split('\n')[0].trim()
      if (reason) stageCompleteReason = reason
      const extraLines = after.split('\n')
      extraLines.shift()
      stageCompleteExtra = extraLines.join('\n').trim()
    }
    stageCompletePending = true
    try { sessionStorage.setItem('wf_advance_pending', '1') } catch (e) { }
    if (before.trim()) appendText(before)
  } else {
    appendText(clean)
  }
}

// ── Message rendering ──
let currentMsgEl: HTMLElement | null = null
let currentTextEl: HTMLElement | null = null
let currentReasoningEl: HTMLElement | null = null

function hideWelcome() {
  const w = document.getElementById('welcome')
  if (w) w.style.display = 'none'
  if (!hasVisibleHistory.value) {
    hasVisibleHistory.value = true
    window.dispatchEvent(new CustomEvent('hasVisibleHistory-changed', { detail: true }))
  }
}
function showWelcome() {
  const w = document.getElementById('welcome')
  if (w) w.style.display = ''
  // Clear log of non-Vue rendered elements
  const log = document.getElementById('log')
  if (log) {
    // Remove tool cards, approvals, asks, compaction, usage strips, notices, phases, errors
    log.querySelectorAll('.card, .approval, .ask, .compaction, .metric-strip, .notice, .phase, .msg--error, .msg--user, .msg--assistant').forEach(el => el.remove())
  }
}

function addUserMsg(text: string) {
  hideWelcome()
  const d = el('div', 'msg msg--user')
  var _body = document.createElement('div'); _body.className = 'msg__body'; var _txt = document.createElement('div'); _txt.className = 'msg__text'; _txt.textContent = text; _body.appendChild(_txt); d.appendChild(_body)
  document.getElementById('log')?.appendChild(d)
  scrollDown(true)
  currentMsgEl = null; currentTextEl = null; currentReasoningEl = null
}

function ensureMsg() {
  if (!currentMsgEl) {
    hideWelcome()
    currentMsgEl = el('div', 'msg msg--assistant')
    document.getElementById('log')?.appendChild(currentMsgEl)
  }
  return currentMsgEl
}

function appendText(t: string) {
  const m = ensureMsg()
  if (!currentTextEl) {
    currentTextEl = document.createElement('span')
    currentTextEl.className = 'msg__text'
    const cursor = m.querySelector('.cursor')
    if (cursor) cursor.remove()
    m.appendChild(currentTextEl)
    m.appendChild(el('span', 'cursor'))
  }
  currentTextEl.textContent += t
  scrollDown()
}

function appendReasoning(t: string) {
  const m = ensureMsg()
  if (!currentReasoningEl) {
    const w = el('div', 'reasoning')
    const b = el('button', 'reasoning__toggle')
    b.innerHTML = '<span class="reasoning__chevron">▶</span> 思考过程'
    const body = el('div', 'reasoning__body')
    body.style.display = 'none'
    b.onclick = () => {
      body.style.display = body.style.display === 'none' ? '' : 'none'
      b.querySelector('.reasoning__chevron')!.className = 'reasoning__chevron' + (body.style.display !== 'none' ? ' reasoning__chevron--open' : '')
    }
    w.appendChild(b)
    w.appendChild(body)
    if (currentTextEl) m.insertBefore(w, currentTextEl)
    else m.appendChild(w)
    currentReasoningEl = body
  }
  currentReasoningEl.textContent += stripSystemTags(t)
  scrollDown()
}

function finalizeMsg() {
  if (currentMsgEl) {
    const c = currentMsgEl.querySelector('.cursor')
    if (c) c.remove()
  }
  currentMsgEl = null; currentTextEl = null; currentReasoningEl = null
}

// ── Tool cards ──
function toolIcon(kind: string) {
  if (kind === 'success') return '<svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><polyline points="20 6 9 17 4 12"/></svg>'
  if (kind === 'danger') return '<svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>'
  return '<svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><circle cx="12" cy="12" r="10" stroke-dasharray="60" stroke-dashoffset="20"/></svg>'
}

function setToolStatus(card: HTMLElement, tone: string, label: string) {
  card.dataset.tone = tone
  const badge = card.querySelector('.card-badge')
  if (badge) badge.textContent = label
  const ico = card.querySelector('.ico')
  if (ico) { ico.className = 'ico' + (tone === 'accent' ? ' spin' : ''); ico.innerHTML = toolIcon(tone) }
}

async function copyText(text: string) {
  if (!text) return false
  if (navigator.clipboard?.writeText) return navigator.clipboard.writeText(text).then(() => true).catch(() => false)
  return false
}

function renderToolDispatch(tool: any) {
  hideWelcome()
  const card = el('div', 'card')
  card.id = 'tool-' + tool.id
  card.dataset.open = 'false'
  card.dataset.tone = 'accent'
  card.dataset.startedAt = String(Date.now())
  const head = el('div', 'card-head')
  const summary = toolArgsSummary(tool.args)
  head.innerHTML = '<span class="ico spin">' + toolIcon('accent') + '</span><div class="card-main"><div class="card-title"><span class="name">' + escHtml(tool.name) + '</span>' + (summary ? '<span class="subject">' + escHtml(summary) + '</span>' : '') + '</div><div class="card-meta">参数 ' + fmtTok(String(tool.args || '').length) + '</div></div><span class="card-badge">运行中</span><div class="card-actions"><button type="button" class="card-action card-copy" title="复制输出" aria-label="复制输出"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg></button><span class="chev"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="6 9 12 15 18 9"/></svg></span></div>'
  const body = el('div', 'card-body')
  body.style.display = 'none'
  head.onclick = (e) => {
    if ((e.target as HTMLElement).closest('button')) return
    const open = card.dataset.open === 'true'
    card.dataset.open = open ? 'false' : 'true'
    body.style.display = open ? 'none' : ''
  }
  const copy = head.querySelector('.card-copy') as HTMLElement
  if (copy) {
    copy.onclick = async (e) => {
      e.stopPropagation()
      const ok = await copyText(body.textContent || tool.args || '')
      copy.title = ok ? '已复制' : '复制输出'
      setTimeout(() => { copy.title = '复制输出' }, 1200)
    }
  }
  card.appendChild(head)
  card.appendChild(body)
  document.getElementById('log')?.appendChild(card)
  toolCards[tool.id] = card
  scrollDown()
}

function renderToolResult(tool: any) {
  const card = toolCards[tool.id]
  if (!card) return
  const elapsed = Math.max(0, Date.now() - Number(card.dataset.startedAt || Date.now()))
  setToolStatus(card, tool.err ? 'danger' : 'success', tool.err ? '失败' : '完成')
  if (tool.err) {
    card.appendChild(el('div', 'err-body', tool.err))
    const meta = card.querySelector('.card-meta')
    if (meta) meta.textContent = fmtElapsed(elapsed)
  } else {
    const body = card.querySelector('.card-body')
    const output = String(tool.output || '')
    if (body) body.textContent = output ? output.slice(0, 2000) + (tool.truncated ? '\n...[truncated]' : '') : '无输出'
    const meta = card.querySelector('.card-meta')
    if (meta) meta.textContent = fmtElapsed(elapsed) + ' · ' + lineCount(output) + ' 行'
  }
  scrollDown()
}

function renderToolProgress(tool: any) {
  const card = toolCards[tool.id]
  if (!card) return
  const body = card.querySelector('.card-body') as HTMLElement
  if (!body) return
  body.style.display = ''
  card.dataset.open = 'true'
  body.textContent += tool.output || ''
  if (body.textContent.length > 4000) body.textContent = body.textContent.slice(-3000)
  const meta = card.querySelector('.card-meta')
  if (meta) meta.textContent = fmtElapsed(Date.now() - Number(card.dataset.startedAt || Date.now())) + ' · ' + lineCount(body.textContent) + ' 行'
  scrollDown()
}

// ── Approval card ──
function showApproval(a: any) {
  const d = el('div', 'approval')
  const actions = [
    '<button class="approval__btn approval__btn--allow" data-allow="true" data-session="false"><span class="approval__key">Y</span> 允许</button>',
    '<button class="approval__btn approval__btn--allow" data-allow="true" data-session="true"><span class="approval__key">A</span> 本会话允许</button>',
    '<button class="approval__btn approval__btn--allow" data-allow="true" data-session="true" data-persist="true"><span class="approval__key">P</span> 总是允许</button>',
    '<button class="approval__btn approval__btn--deny" data-allow="false"><span class="approval__key">N</span> 拒绝</button>',
  ]
  d.innerHTML = '<div class="approval__header"><svg class="approval__icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg><span class="approval__title">需要批准</span></div><div class="approval__subject">' + escHtml(a.tool) + (a.subject ? ' — ' + escHtml(a.subject) : '') + '</div><div class="approval__actions">' + actions.join('') + '</div>'
  document.getElementById('log')?.appendChild(d)
  scrollDown(true)
  const cleanup = () => { pendingPrompts = pendingPrompts.filter(f => f !== cleanup); d.remove() }
  pendingPrompts.push(cleanup)
  const resolve = (payload: any) => {
    fetch('/approve?token=' + encodeURIComponent(localStorage.getItem('teamix_token') || ''), {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(Object.assign({ id: a.id }, payload))
    })
    cleanup()
  }
  d.querySelectorAll('.approval__btn').forEach(btn => {
    btn.addEventListener('click', () => resolve({
      allow: (btn as HTMLElement).dataset.allow === 'true',
      session: (btn as HTMLElement).dataset.session === 'true',
      persist: (btn as HTMLElement).dataset.persist === 'true',
    }))
  })
}

// ── Ask card ──
function showAsk(ask: any) {
  const d = el('div', 'ask')
  const cleanup = () => { pendingPrompts = pendingPrompts.filter(f => f !== cleanup); d.remove() }
  const allSelected: Map<number, Set<number>> = new Map()
  ask.questions.forEach((q: any, qi: number) => {
    const qDiv = el('div')
    qDiv.style.marginBottom = '16px'
    qDiv.appendChild(el('div', 'ask__prompt', q.prompt))
    const opts = el('div', 'ask__options')
    const selected = new Set<number>()
    allSelected.set(qi, selected)
    q.options.forEach((o: any, i: number) => {
      const opt = el('button', 'ask__opt')
      opt.innerHTML = '<div><div class="ask__opt-label">' + escHtml(o.label) + '</div>' + (o.description ? '<div class="ask__opt-desc">' + escHtml(o.description) + '</div>' : '') + '</div>'
      opt.onclick = () => {
        if (q.multi) { selected.has(i) ? selected.delete(i) : selected.add(i); opt.classList.toggle('ask__opt--selected', selected.has(i)) }
        else { selected.clear(); selected.add(i); opts.querySelectorAll('.ask__opt').forEach((oj, j) => oj.classList.toggle('ask__opt--selected', j === i)) }
        // Update submit button state
        const sa = d.querySelector('.ask__submit') as HTMLButtonElement
        if (sa) sa.disabled = !Array.from(allSelected.values()).some(s => s.size > 0)
      }
      opts.appendChild(opt)
    })
    qDiv.appendChild(opts)
    d.appendChild(qDiv)
  })
  // Single submit button for ALL questions
  const submitAll = el('button', 'ask__submit', '提交')
  submitAll.disabled = true
  submitAll.onclick = () => {
    const answers = ask.questions.map((qq: any, qi: number) => ({
      questionId: qq.id,
      selected: Array.from(allSelected.get(qi) || new Set()).map(i => qq.options[i].label)
    }))
    fetch('/answer?token=' + encodeURIComponent(localStorage.getItem('teamix_token') || ''), {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ id: ask.id, answers })
    })
    cleanup()
  }
  d.appendChild(submitAll)
  document.getElementById('log')?.appendChild(d)
  scrollDown(true)
  pendingPrompts.push(cleanup)
}
// ── Compaction ──
function showCompaction(c: any) {
  const d = el('div', 'compaction')
  if (c.summary) {
    const head = el('div', 'compaction__head')
    head.innerHTML = '<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="4 14 10 14 10 20"/><polyline points="20 10 14 10 14 4"/><line x1="14" y1="10" x2="21" y2="3"/><line x1="3" y1="21" x2="10" y2="14"/></svg>'
    head.appendChild(el('span', 'compaction__title', '已压缩'))
    head.appendChild(el('span', '', c.messages + ' 条消息'))
    const body = el('div', 'compaction__body', c.summary)
    head.onclick = () => body.classList.toggle('compaction__body--open')
    d.appendChild(head); d.appendChild(body)
  } else {
    d.textContent = '压缩中...'
  }
  document.getElementById('log')?.appendChild(d)
  scrollDown()
}

// ── Usage strip ──
function showUsageStrip(usage: any) {
  try { sessionStorage.setItem('teamix_last_usage', JSON.stringify(usage)) } catch {}
  const log = document.getElementById('log')
  if (!log) return
  const strip = el('div', 'metric-strip')
  const items = [
    { l: '总计', v: fmtTok(usage.totalTokens), c: '' },
    { l: '输入', v: fmtTok(usage.promptTokens), c: 'acc' },
    { l: '输出', v: fmtTok(usage.completionTokens), c: 'ok' },
  ]
  items.forEach(it => {
    const sp = el('span', 'item')
    sp.innerHTML = it.l + ' <span class="v ' + it.c + '">' + it.v + '</span>'
    strip.appendChild(sp)
  })
  if (usage.cacheHitTokens) {
    const sp = el('span', 'item')
    sp.innerHTML = '缓存 <span class="v acc">' + Math.round(usage.cacheHitTokens / Math.max(1, usage.cacheHitTokens + usage.cacheMissTokens) * 100) + '%</span>'
    strip.appendChild(sp)
  }
  const cost = usage.cost ?? usage.costUsd
  if (typeof cost === 'number' && cost > 0) {
    const sp = el('span', 'item')
    sp.innerHTML = '费用 <span class="v">' + fmtMoney(cost, usage.currency) + '</span>'
    strip.appendChild(sp)
  }
  log.appendChild(strip)
  scrollDown()
}

// ── Notice / Phase / Error ──
function showNotice(text: string, tone?: string) {
  hideWelcome()
  const n = el('div', 'notice' + (tone === 'warn' ? ' notice--warn' : ''), text)
  document.getElementById('log')?.appendChild(n)
  scrollDown(true)
}

function showPhase(text: string) {
  document.getElementById('log')?.appendChild(el('div', 'phase', text))
  scrollDown()
}

function showError(err: string) {
  const log = document.getElementById('log')
  if (log) log.appendChild(el('div', 'msg--error', '✗ ' + err))
  scrollDown()
}

// ── Open page card ──
function showOpenPageCard(pages: { url: string; label: string }[]) {
  const d = el('div', 'approval')
  d.style.borderLeft = '3px solid var(--accent)'
  d.style.marginBottom = '8px'
  const btns = pages.map((p, i) => '<button class="approval__btn approval__btn--allow" id="opg-' + i + '"><span class="approval__key">' + (i + 1) + '</span> 打开 ' + escHtml(p.label) + '</button>').join('')
  const list = pages.map(p => '<div style="font-family:var(--mono);font-size:11px;color:var(--fg-2);padding:2px 0">• ' + p.url + ' <span style="color:var(--muted-2)">(' + p.label + ')</span></div>').join('')
  d.innerHTML = '<div class="approval__header"><svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/><polyline points="15 3 21 3 21 9"/><line x1="10" y1="14" x2="21" y2="3"/></svg><span class="approval__title">查看效果</span></div><div class="approval__subject">修改已完成，请检查效果：</div><div style="padding:6px 12px 8px;border-bottom:1px solid var(--border);background:var(--bg-2)">' + list + '</div><div class="approval__actions">' + btns + '<button class="approval__btn approval__btn--deny" id="opg-dismiss" style="margin-left:auto">暂不打开</button></div>'
  document.getElementById('log')?.appendChild(d)
  scrollDown(true)
  pendingPrompts.push(() => { if (d.parentNode) d.remove() })
  pages.forEach((p, i) => {
    document.getElementById('opg-' + i)?.addEventListener('click', () => { window.open(p.url, '_blank') })
  })
  document.getElementById('opg-dismiss')?.addEventListener('click', () => { d.remove() })
}

// ── Todo panel ──
function parseTodos(args: any) {
  try { const a = JSON.parse(args); return Array.isArray(a.todos) ? a.todos : [] }
  catch { return [] }
}

function renderTodoPanel() {
  if (todosDismissed) { showTodoPanel.value = false; return }
  if (!todosState.length) { showTodoPanel.value = false; return }
  showTodoPanel.value = true
}

function fetchTodos() {
  fetch('/todos?token=' + encodeURIComponent(localStorage.getItem('teamix_token') || ''))
    .then(r => r.json()).then(ts => { if (Array.isArray(ts)) { todosState = ts; renderTodoPanel() } })
    .catch(() => { })
}

// ── Workflow ──
async function loadWorkflow() {
  try {
    const data = await api.workflow()
        if (data && data.stages && data.stages.length > 0) {
      wfStages.value = data.stages
      wfVisible.value = true
      // Calculate active stage index: track furthest completed/in_progress
      activeStageIdx = -1
      for (let j = 0; j < data.stages.length; j++) {
        if (data.stages[j].status === 'in_progress') { activeStageIdx = j; break }
        if (data.stages[j].status === 'completed') activeStageIdx = j
      }
      // Restore workflow name from localStorage on page refresh
      if (!wfName.value || wfName.value === '-') {
        const saved = localStorage.getItem('teamix_wf_name')
        if (saved) {
          wfName.value = saved
          const el = document.getElementById('welcome-wf')
          if (el) el.textContent = saved
        }
      }
    } else { wfVisible.value = false; wfName.value = '' }
  } catch { wfVisible.value = false; wfName.value = '' }}

async function setStage(stage: string) {
  if (running.value) return
  showWfConfirm('确认切换到阶段？', async (ok: boolean) => {
    if (!ok) return
    try { await api.workflowSetStage(stage); await loadWorkflow() } catch { }
  })
}

// ── Mode buttons ──
async function setPlan(on: boolean) {
  planMode.value = on
  try { await api.plan(on); fetchStatus() } catch { }
}

async function setToolApprovalMode(mode: string) {
  bypassMode.value = mode === 'yolo'
  try {
    await fetch('/tool-approval-mode?token=' + encodeURIComponent(localStorage.getItem('teamix_token') || ''), {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ mode })
    })
    fetchStatus()
  } catch {}
}

async function toggleYolo() {
  if (bypassMode.value) { await setToolApprovalMode('ask') }
  else { await setToolApprovalMode('yolo') }
  setTimeout(fetchStatus, 200)
}

function toggleGoalMode() {
  if (goalActive.value) {
    fetch('/goal?token=' + encodeURIComponent(localStorage.getItem('teamix_token') || ''), {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ goal: '' })
    }).then(() => { goalActive.value = false; goalText.value = ''; fetchStatus() })
    return
  }
  if (goalMode.value) { goalMode.value = false } else { goalMode.value = true }
  inputText.value = ''
  nextTick(() => document.getElementById('in')?.focus())
}

// ── Input handling ──
async function syncModeBeforeSubmit() {
  const t = localStorage.getItem('teamix_token')
  if (!t) return
  try {
    await fetch('/plan?token=' + encodeURIComponent(t), {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ on: planMode.value })
    })
    await fetch('/tool-approval-mode?token=' + encodeURIComponent(t), {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ mode: bypassMode.value ? 'yolo' : 'ask' })
    })
  } catch { }
}

function expandPastedText(text: string): string {
  let result = text
  for (const block of pastedBlocks.value) {
    if (result.includes(block.label)) {
      result = result.split(block.label).join(block.label + '\n\n--- Begin ' + block.label + ' ---\n' + block.text + '\n--- End ' + block.label + ' ---')
    }
  }
  return result
}


async function send() {
  const v = expandPastedText(inputText.value.trim())
  if (!v) return
  await syncModeBeforeSubmit()

  // Workflow stage complete pending
  if (stageCompletePending) {
    try { sessionStorage.removeItem('wf_advance_pending') } catch (e) { }
    addUserMsg(v)
    chatHistory.push(v)
    if (chatHistory.length > 50) chatHistory.shift()
    chatHistoryIndex = chatHistory.length
    inputText.value = ''
    const reasonText = stageCompleteReason ? '（' + stageCompleteReason + '）' : ''
    showWfConfirm('阶段已完成' + reasonText + '，是否进入下一阶段？', (ok: boolean) => {
      if (!ok) { stageCompletePending = false; stageCompleteReason = ''; stageCompleteExtra = ''; return }
      const t = localStorage.getItem('teamix_token')
      if (!t) return
      fetch('/teamix/workflow/advance?token=' + encodeURIComponent(t), {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}'
      }).then(() => { loadWorkflow(); (async () => { const tk = localStorage.getItem('teamix_token'); if(tk) await fetch('/submit?token='+encodeURIComponent(tk),{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({input:v})}) })() }).catch(() => { loadWorkflow(); (async () => { const tk = localStorage.getItem('teamix_token'); if(tk) await fetch('/submit?token='+encodeURIComponent(tk),{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({input:'请继续'})}) })() })
    })
    return
  }

  // Workflow commands
  const lowerV = v.toLowerCase()

  let submitInput = v
  if (goalMode.value && !v.startsWith('/goal')) {
    submitInput = '/goal ' + v
    goalMode.value = false
  } else if (goalMode.value) {
    goalMode.value = false
  }

  addUserMsg(v)
  chatHistory.push(v)
  if (chatHistory.length > 50) chatHistory.shift()
  chatHistoryIndex = chatHistory.length
  inputText.value = ''
  running.value = true
  statusText.value = "思考中..."
  // Post and forget - SSE handles the response
  const t = localStorage.getItem('teamix_token')
  if (t) {
    fetch('/submit?token=' + encodeURIComponent(t), {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ input: submitInput })
    }).then(r => {
      if (r.ok && (r.status === 204 || r.status === 202)) {
        fetchStatus()
      }
    }).catch(() => {})
  }
}

async function doStop() {
  try { await api.cancel() } catch { }
  running.value = false
  statusText.value = "已取消"
}

// ── Rewind picker ──
function openRewindPicker() {
  fetch('/checkpoints?token=' + encodeURIComponent(localStorage.getItem('teamix_token') || ''))
    .then(r => r.json()).then(cps => {
      checkpointCount = Array.isArray(cps) ? cps.length : 0
      if (!cps || cps.length === 0) { showNotice('暂无可用检查点', 'warn'); return }
      rewindCheckpoints = cps; rewindStage = 0; rewindSelected = 0; rewindScope = 0
      rewindKey.value++; showRewind.value = true
    }).catch(() => { })
}

function selectRewindCheckpoint(i: number) { rewindSelected = i; rewindKey.value++ }
function advanceRewindStage() { rewindStage = 1; rewindScope = 0; rewindKey.value++ }

function applyRewind() {
  const cp = rewindCheckpoints[rewindSelected]
  const sc = SCOPES[rewindScope]
  showRewind.value = false
  const t = localStorage.getItem('teamix_token')
  if (!t) return
  const q = '?token=' + encodeURIComponent(t)
  if (sc.scope === 'fork') {
    fetch('/fork' + q, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ turn: cp.turn, name: '' }) })
  } else if (sc.scope === 'sumfrom') {
    fetch('/summarize' + q, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ turn: cp.turn, mode: 'from' }) })
  } else if (sc.scope === 'sumupto') {
    fetch('/summarize' + q, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ turn: cp.turn, mode: 'upto' }) })
  } else {
    fetch('/rewind' + q, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ turn: cp.turn, scope: sc.scope }) })
  }
}

function refreshCheckpointAvailability() {
  fetch('/checkpoints?token=' + encodeURIComponent(localStorage.getItem('teamix_token') || ''))
    .then(r => r.json()).then(cps => { checkpointCount = Array.isArray(cps) ? cps.length : 0 }).catch(() => { })
}

// ── Status ──
function fetchStatus() {
  api.status().then(s => {
    if (!s) return
    statusModel.value = s.label || '-'
    const wm = document.getElementById('welcome-model'); if (wm) wm.textContent = s.label || '-'
    if (s.window && s.used !== undefined) {
      window.dispatchEvent(new CustomEvent('status-update', { detail: s }))
    }
    // Expose cumulative stats for StatsModal\n  (window as any)._cumulativeStats = { cost: cumulativeCost, tokens: cumulativeCost > 0 ? cumulativeCost * 1000 : 0, cacheHit: cumulativeCacheHit, cacheMiss: cumulativeCacheMiss }\n  planMode.value = !!s.plan
    const tam = s.toolApprovalMode || ((s.autoApproveTools ?? s.bypass) ? 'yolo' : 'ask')
    bypassMode.value = tam === 'yolo'
    goalText.value = (s.goal || '').trim()
    goalActive.value = goalText.value !== '' && (s.goalStatus || '') === 'running'
    // Fetch workspace root from /teamix/project (not from status.cwd which is sessions dir)
    if (cwd.value === '-') {
      api.project().then(data => {
        if (data && data.workspaceRoot) {
          const fullPath = data.workspaceRoot.replace(/\\/g, '/')
          let parts = fullPath.split('/').filter(Boolean)
          if (parts.length > 0 && parts[0].endsWith(':')) parts[0] = parts[0].slice(0, -1)
          cwdTitle.value = '/' + parts.join('/')
          if (parts.length <= 2) { cwd.value = '/' + parts.join('/') }
          else { cwd.value = '/' + parts.slice(0, 2).join('/') + '/.../' + parts.slice(-2).join('/') }
        }
      }).catch(() => {})
    }
  }).catch(() => { })
}

// ── Session list ──
function loadSessions() {
  api.sessions().then(ss => {
    window.dispatchEvent(new CustomEvent('sessions-update', { detail: ss }))
  }).catch(() => { })
}

// ── Notifications ──
function fetchNotifications() {
  api.notifications().then(data => {
    if (!Array.isArray(data)) return
    window.dispatchEvent(new CustomEvent('notifications-update', { detail: data }))
  }).catch(() => { })
}

// ── Wf confirm ──
function showJumpCard() {
  const d = el('div', 'approval')
  d.style.borderLeft = '3px solid var(--accent)'
  d.style.marginBottom = '8px'
  d.innerHTML = '<div class="approval__header"><svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg><span class="approval__title">跳转到阶段</span></div><div class="approval__subject">请选择要跳转到的阶段：</div><div id="jump-stage-list" style="padding:6px 12px;border-bottom:1px solid var(--border);background:var(--bg-2)"></div><div class="approval__actions"><button class="approval__btn approval__btn--deny" id="jump-cancel">取消</button></div>'
  document.getElementById('log')?.appendChild(d)
  scrollDown(true)
  document.getElementById('jump-cancel')!.onclick = () => { d.remove() }
  const list = document.getElementById('jump-stage-list')!
  list.innerHTML = '<div style="color:var(--muted-2);padding:4px 0">加载中...</div>'
  const t = localStorage.getItem('teamix_token')
  if (!t) return
  fetch('/teamix/workflow?token=' + encodeURIComponent(t)).then(r => r.json()).then(data => {
    if (!data || !data.stages || data.stages.length === 0) { list.innerHTML = '<div style="color:var(--muted-2);padding:4px 0">无可用阶段</div>'; return }
    let html = ''
    data.stages.forEach((s: any) => {
      const active = s.status === 'in_progress' ? ' style="background:var(--accent-soft);color:var(--accent);font-weight:500"' : ''
      html += '<div class="wf-jump-item" data-stage="' + s.stage + '"' + active + ' style="padding:5px 8px;cursor:pointer;border-radius:4px;margin:2px 0">' + (s.label || s.stage) + ' <span style="color:var(--muted-2);font-size:10px">(' + (s.status === 'in_progress' ? '当前' : s.status === 'completed' ? '已完成' : '待开始') + ')</span></div>'
    })
    list.innerHTML = html
    list.querySelectorAll('.wf-jump-item').forEach((el) => {
      (el as HTMLElement).onclick = () => {
        const sn = el.getAttribute('data-stage')
        const tk = localStorage.getItem('teamix_token')
        if (!sn || !tk) return
        fetch('/teamix/workflow/setstage?token=' + encodeURIComponent(tk), { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ stage: sn }) })
          .then(r => {
            if (r.ok) { d.remove(); loadWorkflow() }
          })
      }
    })
  }).catch(() => { list.innerHTML = '<div style="color:#f44336;padding:4px 0">加载失败</div>' })
}

function showStageApproval(reason: string) {
  const extra = stageCompleteExtra ? '\n\n' + stageCompleteExtra : ''
  const msg = (reason ? 'AI认为当前阶段已完成：' + reason : 'AI认为当前阶段已完成') + extra
  const d = el('div', 'approval')
  d.style.borderLeft = '3px solid var(--accent)'
  d.style.marginBottom = '8px'
  d.innerHTML = '<div class="approval__header"><svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg><span class="approval__title">阶段完成</span></div><div class="approval__subject">' + msg + '</div><div class="approval__actions"><button class="approval__btn approval__btn--allow" id="sa-confirm"><span class="approval__key">Y</span> 确认进入下一阶段</button><button class="approval__btn approval__btn--deny" id="sa-cancel"><span class="approval__key">N</span> 继续当前阶段</button><button class="approval__btn" id="sa-jump" style="border:1px solid var(--border);color:var(--fg-2);font-size:11px"><span class="approval__key">J</span> 跳转到...</button></div><div id="sa-jump-list" style="display:none;border-top:1px solid var(--border);padding:8px 12px;font-size:12px"></div>'
  document.getElementById('log')?.appendChild(d)
  scrollDown(true)
  document.getElementById('sa-confirm')!.onclick = () => {
    stageCompletePending = false; stageCompleteReason = ''; stageCompleteExtra = ''
    d.remove()
    const t = localStorage.getItem('teamix_token')
    if (!t) return
    fetch('/teamix/workflow/advance?token=' + encodeURIComponent(t), { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' })
      .then(() => { loadWorkflow(); fetch('/submit?token=' + encodeURIComponent(t), { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ input: '请继续' }) }) })
      .catch(() => { loadWorkflow(); fetch('/submit?token=' + encodeURIComponent(t), { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ input: '请继续' }) }) })
  }
  document.getElementById('sa-cancel')!.onclick = () => {
    stageCompletePending = false; stageCompleteReason = ''; stageCompleteExtra = ''
    d.remove()
  }
  document.getElementById('sa-jump')!.onclick = () => {
    const list = document.getElementById('sa-jump-list')!
    if (list.style.display !== 'none') { list.style.display = 'none'; return }
    list.innerHTML = '<div style="color:var(--muted-2);padding:4px 0">加载中...</div>'
    list.style.display = 'block'
    const t = localStorage.getItem('teamix_token')
    if (!t) return
    fetch('/teamix/workflow?token=' + encodeURIComponent(t)).then(r => r.json()).then(data => {
      if (!data || !data.stages || data.stages.length === 0) { list.innerHTML = '<div style="color:var(--muted-2);padding:4px 0">无可用阶段</div>'; return }
      let html = ''
      data.stages.forEach((s: any) => {
        const active = s.status === 'in_progress' ? ' style="background:var(--accent-soft);color:var(--accent);font-weight:500"' : ''
        html += '<div class="wf-jump-item" data-stage="' + s.stage + '"' + active + ' style="padding:5px 8px;cursor:pointer;border-radius:4px;margin:2px 0">' + (s.label || s.stage) + ' <span style="color:var(--muted-2);font-size:10px">(' + s.status + ')</span></div>'
      })
      list.innerHTML = html
      list.querySelectorAll('.wf-jump-item').forEach((el) => {
        (el as HTMLElement).onclick = () => {
          const sn = el.getAttribute('data-stage')
          const tk = localStorage.getItem('teamix_token')
          if (!sn || !tk) return
          fetch('/teamix/workflow/setstage?token=' + encodeURIComponent(tk), { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ stage: sn }) })
            .then(r => { if (r.ok) { stageCompletePending = false; d.remove(); loadWorkflow(); fetch('/submit?token=' + encodeURIComponent(tk), { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ input: '请继续' }) }) } })
        }
      })
    }).catch(() => { list.innerHTML = '<div style="color:#f44336;padding:4px 0">加载失败</div>' })
  }
}

function showWfConfirm(msg: string, callback: (ok: boolean) => void) {
  const d = el('div', 'approval')
  d.style.borderLeft = '3px solid var(--accent)'
  d.style.marginBottom = '8px'
  d.innerHTML = '<div class="approval__header"><svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg><span class="approval__title">工作流确认</span></div><div class="approval__subject">' + msg + '</div><div class="approval__actions"><button class="approval__btn approval__btn--allow" id="wfc-yes"><span class="approval__key">Y</span> 确认</button><button class="approval__btn approval__btn--deny" id="wfc-no"><span class="approval__key">N</span> 取消</button></div>'
  document.getElementById('log')?.appendChild(d)
  scrollDown(true)
  const cleanup = () => { d.remove() }
  document.getElementById('wfc-yes')!.onclick = () => { cleanup(); callback(true) }
  document.getElementById('wfc-no')!.onclick = () => { cleanup(); callback(false) }
}

// ── Set running / conn state helpers ──
function setRunning(on: boolean) {
  running.value = on
  statusText.value = on ? (goalActive.value ? '活跃目标 · ' : '') + '思考中...' : '就绪'
  if (on) {
    turnStartAt = Date.now()
    turnTokens = 0
    tickTimer = setInterval(() => {
      const ms = Date.now() - turnStartAt
      const tok = turnTokens > 0 ? ' · ↓ ' + fmtTok(turnTokens) + ' tok' : ''
      const ti = document.getElementById('turn-info')
      if (ti) ti.textContent = fmtElapsed(ms) + tok
    }, 1000)
  } else {
    if (tickTimer) { clearInterval(tickTimer); tickTimer = null }
    const ti = document.getElementById('turn-info')
    if (ti) ti.textContent = ''
  }
}

function setConnState(state: string) {
  const colors: Record<string, string> = { connected: 'var(--success)', reconnecting: 'var(--warning)', disconnected: 'var(--danger)' }
  const dots = document.querySelectorAll('.status__dot')
  dots.forEach(dot => {
    if (!running.value) { (dot as HTMLElement).style.background = colors[state] || '' }
  })
  if (!running.value) {
    statusText.value = { connected: '已连接', reconnecting: '重新连接...', disconnected: '已断开' }[state] || state
  }
}

let retryStatus: { attempt: number; max: number } | null = null
function setRetrying(attempt: number, max: number) {
  retryStatus = { attempt, max }
  statusText.value = '正在重试 (' + attempt + '/' + max + ')...'
}
function clearRetrying() {
  if (!retryStatus) return
  retryStatus = null
  if (running.value) statusText.value = '思考中...'
}

// ── Scroll ──
let pinnedToBottom = true
function scrollDown(force?: boolean) {
  const log = document.getElementById('log')
  if (!log) return
  if (force) pinnedToBottom = true
  if (!pinnedToBottom) return
  requestAnimationFrame(() => {
    if (!pinnedToBottom && !force) return
    log.scrollTo({ top: log.scrollHeight, behavior: 'instant' })
  })
}

// ── Global keydown ──
function onGlobalKeydown(e: KeyboardEvent) {
  // rewind picker nav
  if (showRewind.value) {
    if (e.key === 'Escape') {
      if (rewindStage === 0) { showRewind.value = false }
      else { rewindStage = 0 }
      e.preventDefault(); return
    }
    if (rewindStage === 0) {
      if (e.key === 'j' || e.key === 'ArrowDown') { rewindSelected = Math.min(rewindSelected + 1, rewindCheckpoints.length - 1); rewindKey.value++; e.preventDefault(); return }
      if (e.key === 'k' || e.key === 'ArrowUp') { rewindSelected = Math.max(rewindSelected - 1, 0); rewindKey.value++; e.preventDefault(); return }
      if (e.key === 'Enter') { rewindStage = 1; rewindScope = 0; rewindKey.value++; e.preventDefault(); return }
    } else {
      if (e.key === 'j' || e.key === 'ArrowDown') { rewindScope = Math.min(rewindScope + 1, SCOPES.length - 1); rewindKey.value++; e.preventDefault(); return }
      if (e.key === 'k' || e.key === 'ArrowUp') { rewindScope = Math.max(rewindScope - 1, 0); rewindKey.value++; e.preventDefault(); return }
      if (e.key === 'Enter') { applyRewind(); e.preventDefault(); return }
      const idx = SCOPES.findIndex(s => s.key === e.key)
      if (idx >= 0) { rewindScope = idx; applyRewind(); e.preventDefault(); return }
    }
  }
  // slash menu nav
  if (slashOpen.value && document.activeElement === document.getElementById('in')) {
    if (e.key === 'ArrowDown') { e.preventDefault(); slashIndex = Math.min(slashIndex + 1, slashFiltered.length - 1); updateSlashMenu(); return }
    if (e.key === 'ArrowUp') { e.preventDefault(); slashIndex = Math.max(slashIndex - 1, 0); updateSlashMenu(); return }
    if (e.key === 'Tab' || e.key === 'Enter') { e.preventDefault(); acceptSlash(); return }
    if (e.key === 'Escape') { e.preventDefault(); closeSlashMenu(); return }
  }

  const inp = document.getElementById('in')
  if (e.target === inp && e.key === 'Tab' && e.shiftKey) { e.preventDefault(); cycleMode(); return }
  if (e.target === inp && (e.key === 'y' || e.key === 'Y') && (e.ctrlKey || e.metaKey) && !e.altKey && !e.shiftKey) { e.preventDefault(); toggleYolo(); return }
  if (e.key === '/' && !e.ctrlKey && !e.metaKey && !e.altKey && document.activeElement !== inp) { e.preventDefault(); inp?.focus() }
}

// ── Slash menu ──
function updateSlashMenu() {
  const v = inputText.value
  if (!v.startsWith('/') || v.includes(' ')) { closeSlashMenu(); return }
  const q = v.slice(1).toLowerCase()
  slashFiltered = SLASH_CMDS.filter(c => {
    const hay = [c.cmd, c.sig, c.desc, c.group].join(' ').toLowerCase()
    return hay.includes(q)
  }).sort((a, b) => {
    const ap = a.cmd.startsWith(q) ? 0 : 1, bp = b.cmd.startsWith(q) ? 0 : 1
    return ap - bp || SLASH_CMDS.indexOf(a) - SLASH_CMDS.indexOf(b)
  })
  if (slashFiltered.length === 0) { closeSlashMenu(); return }
  slashOpen.value = true
  slashIndex = 0
}

function closeSlashMenu() { slashOpen.value = false }
function acceptSlash() {
  if (!slashOpen.value) return
  const c = slashFiltered[slashIndex]
  if (c) { inputText.value = '/' + c.cmd + ' ' }
  closeSlashMenu()
  document.getElementById('in')?.focus()
}

function cycleMode() {
  if (goalMode.value) { goalMode.value = false; return }
  setPlan(!planMode.value)
  setTimeout(fetchStatus, 200)
}

// ── Send example ──
async function sendExample(text: string) {
  inputText.value = text
  await send()
}

// ── Input events ──
function onInputKeydown(e: KeyboardEvent) {
  // chat history arrows
  if (e.key === 'ArrowUp' && chatHistory.length > 0) { e.preventDefault(); chatHistoryIndex = Math.max(0, chatHistoryIndex - 1); inputText.value = chatHistory[chatHistoryIndex]; return }
  if (e.key === 'ArrowDown' && chatHistory.length > 0) { e.preventDefault(); chatHistoryIndex = Math.min(chatHistory.length, chatHistoryIndex + 1); inputText.value = chatHistoryIndex < chatHistory.length ? chatHistory[chatHistoryIndex] : ''; return }
  if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); send(); return }
  if (e.key === 'Escape') {
    if (goalMode.value && !running.value) { goalMode.value = false; inputText.value = ''; closeSlashMenu(); return }
    if (running.value) { doStop(); return }
    if (inputText.value === '') {
      if (escTimer) { clearTimeout(escTimer); escTimer = null; openRewindPicker() }
      else { escTimer = setTimeout(() => { escTimer = null }, 600) }
      return
    }
  }
}

function onInput() {
  // auto-resize
  const inp = document.getElementById('in')
  if (inp) {
    inp.style.height = 'auto'
    inp.style.height = Math.min(inp.scrollHeight, 140) + 'px'
  }
  updateSlashMenu()
}

// ── File drop ──
function handlePaste(e: ClipboardEvent) {
  const pasted = e.clipboardData?.getData('text/plain') || ''
  if (!pasted) return
  const lines = pasted.split('\n').length
  if (pasted.length >= 2000 || lines >= 20) {
    e.preventDefault()
    const id = pasteIdCounter++
    const label = '[\u5df2\u7c98\u8d34\u6587\u672c #' + id + ' - ' + lines + ' \u884c]'
    const selStart = (e.target as HTMLTextAreaElement).selectionStart
    const selEnd = (e.target as HTMLTextAreaElement).selectionEnd
    const before = inputText.value.slice(0, selStart)
    const after = inputText.value.slice(selEnd)
    inputText.value = before + label + after
    pastedBlocks.value = [...pastedBlocks.value, { label, text: pasted }]
    return
  }
}


function handleFileDrop(e: DragEvent) {
  e.preventDefault()
  const inp = document.getElementById('in') as HTMLTextAreaElement
  if (!inp) return
  const items = e.dataTransfer?.items
  const files = e.dataTransfer?.files
  if (items && items.length > 0 && files && files.length > 0) {
    const entry = items[0].webkitGetAsEntry ? items[0].webkitGetAsEntry() : null
    if (entry && entry.isDirectory) {
      inp.value = '上传中...'
      inp.disabled = true
      readAndUploadFolder(entry, '', (uploaded: string[]) => {
        inp.disabled = false
        inp.value = inputText.value + (uploaded.length > 0 ? '@' + uploaded.join('\n@') : '')
        inp.focus()
      })
      return
    }
    inp.value = '上传中...'
    inp.disabled = true
    uploadFiles(Array.from(files), (uploaded: string[]) => {
      inp.disabled = false
      inp.value = inputText.value + (uploaded.length > 0 ? '@' + uploaded.join('\n@') : '')
      inp.focus()
    })
    return
  }
  const text = e.dataTransfer?.getData('text/plain')
  if (text) {
    inp.value = inputText.value + text
    inp.focus()
  }
}

function uploadFile(file: File, relPath: string, callback: (path: string | null) => void) {
  const t = localStorage.getItem('teamix_token')
  if (!t) { callback(null); return }
  const fd = new FormData()
  fd.append('file', file)
  fd.append('path', relPath || file.name)
  const xhr = new XMLHttpRequest()
  xhr.open('POST', '/teamix/upload?token=' + encodeURIComponent(t), true)
  xhr.onload = () => {
    if (xhr.status === 200) {
      try { const resp = JSON.parse(xhr.responseText); callback(resp.path || relPath || file.name) }
      catch (e) { callback(file.name) }
    } else { callback(file.name) }
  }
  xhr.onerror = () => { callback(file.name) }
  xhr.send(fd)
}

function uploadFiles(files: File[], callback: (uploaded: string[]) => void) {
  const uploaded: string[] = []
  let pending = files.length
  if (pending === 0) { callback(uploaded); return }
  files.forEach(file => {
    const relPath = (file as any).webkitRelativePath || file.name
    uploadFile(file, relPath, (path) => {
      if (path) uploaded.push(path)
      pending--
      if (pending === 0) callback(uploaded)
    })
  })
}

function readAndUploadFolder(entry: any, path: string, callback: (uploaded: string[]) => void) {
  const reader = entry.createReader()
  const allFiles: any[] = []
  const readBatch = () => {
    reader.readEntries((entries: any[]) => {
      if (entries.length === 0) {
        if (allFiles.length === 0) { callback([]); return }
        const uploaded: string[] = []
        let pending = allFiles.length
        allFiles.forEach((fileEntry: any) => {
          fileEntry.file((file: File) => {
            uploadFile(file, fileEntry.uploadPath, (p) => {
              if (p) uploaded.push(p)
              pending--
              if (pending === 0) callback(uploaded)
            })
          }, () => { pending--; if (pending === 0) callback(uploaded) })
        })
        return
      }
      let p = entries.length
      entries.forEach((entry: any) => {
        const relPath = path ? path + '/' + entry.name : entry.name
        if (entry.isDirectory) {
          readAndUploadFolder(entry, relPath, (files) => {
            allFiles.push(...files)
            p--
            if (p === 0) readBatch()
          })
        } else {
          allFiles.push({ file: entry, uploadPath: relPath })
          p--
          if (p === 0) readBatch()
        }
      })
    })
  }
  readBatch()
}

// ── Expose ──
function togglePastedBlock(label: string) {
  if (openPastedLabels.value.includes(label)) {
    openPastedLabels.value = openPastedLabels.value.filter(l => l !== label)
  } else {
    openPastedLabels.value = [...openPastedLabels.value, label]
  }
}
function removePastedBlock(block: { label: string; text: string }) {
  pastedBlocks.value = pastedBlocks.value.filter(b => b.label !== block.label)
  openPastedLabels.value = openPastedLabels.value.filter(l => l !== block.label)
  inputText.value = inputText.value.replace(block.label, '')
}const previewRef = ref<HTMLElement | null>(null)
let previewResizing = false
let previewStartY = 0
let previewStartH = 0

function startPreviewResize(e: MouseEvent) {
  previewResizing = true
  previewStartY = e.clientY
  previewStartH = previewHeight.value
  document.addEventListener('mousemove', onPreviewResize)
  document.addEventListener('mouseup', stopPreviewResize)
  e.preventDefault()
}
function onPreviewResize(e: MouseEvent) {
  if (!previewResizing) return
  const delta = previewStartY - e.clientY
  previewHeight.value = Math.max(60, Math.min(400, previewStartH + delta))
}
function stopPreviewResize() {
  previewResizing = false
  document.removeEventListener('mousemove', onPreviewResize)
  document.removeEventListener('mouseup', stopPreviewResize)
}


const composerResizeState = { active: false, startY: 0, startH: 0 }
function startComposerResize(e: MouseEvent) {
  const composer = document.getElementById('composer')
  if (!composer) return
  // Fix height immediately to prevent layout shift
  const h = composer.offsetHeight
  if (!composerDefaultHeight) composerDefaultHeight = h
  composer.style.height = h + 'px'
  // Directly add class to input-row for instant layout
  const row = composer.querySelector('.composer__input-row')
  composer.classList.add('composer--resized')

  if (row) row.classList.add('composer__input-row--resized')
  composerResizeState.active = true
  composerResizeState.startY = e.clientY
  composerResizeState.startH = h
  document.addEventListener('mousemove', onComposerResize)
  document.addEventListener('mouseup', stopComposerResize)
  e.preventDefault()
}
function onComposerResize(e: MouseEvent) {
  if (!composerResizeState.active) return
  const delta = composerResizeState.startY - e.clientY
  // 下限 = 首次记录的默认高度（允许缩回初始大小），上限 400
  const minH = composerDefaultHeight || 40
  const newH = Math.max(minH, Math.min(400, composerResizeState.startH + delta))
  const composer = document.getElementById('composer')
  if (composer) composer.style.height = newH + 'px'
}
function stopComposerResize() {
  composerResizeState.active = false
  document.removeEventListener('mousemove', onComposerResize)
  document.removeEventListener('mouseup', stopComposerResize)
}

defineExpose({ loadSessions, fetchStatus, fetchNotifications })
</script>

<template>
  <section class="transcript" id="log">
    <!-- Welcome -->
    <div class="welcome" id="welcome">
      <div class="welcome__brand"><svg width="240" height="56" viewBox="0 0 240 56"><text x="20" y="40" font-size="32" font-weight="700" fill="var(--fg)">Teamix</text><text x="140" y="40" font-size="20" fill="var(--accent)" font-weight="600">Cloud</text></svg></div>
      <div class="welcome__tag">AI 协作开发平台</div>
      <div class="welcome__meta">
        <div class="welcome__pill"><strong>模型</strong><span id="welcome-model">{{ statusModel }}</span></div>
        <div class="welcome__pill"><strong>工作区</strong><span id="welcome-cwd" :title="cwdTitle" style="max-width:180px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;display:inline-block;vertical-align:bottom;cursor:default">{{ cwd }}</span></div>
        <div class="welcome__pill" v-show="wfName && wfName !== '-'"><strong>工作流</strong><span id="welcome-wf">{{ wfName || '-' }}</span></div>
      </div>
      <div class="welcome__hints">
        <span><kbd>/</kbd> 命令</span>
        <span><kbd>Shift+Tab</kbd> 计划</span>
        <span><kbd>Ctrl+Y</kbd> YOLO</span>
        <span><kbd>Esc Esc</kbd> 回退</span>
      </div>
      <div class="welcome__examples">
        <button class="welcome__ex" @click="sendExample('解释项目结构')">解释项目结构</button>
        <button class="welcome__ex" @click="sendExample('查找并修复错误')">查找并修复 bug</button>
        <button class="welcome__ex" @click="sendExample('为主模块编写测试')">编写测试</button>
      </div>
    </div>

    <!-- Rendered messages -->
    <div v-for="(m, i) in messages" :key="i">
      <div v-if="m.role === 'user'" class="msg msg--user">
        <div class="msg__body">
          <div class="msg__text" v-text="m.content"></div>
        </div>
      </div>
      <div v-else-if="m.role === 'assistant'" class="msg msg--assistant">
        <div v-if="m.reasoning" class="reasoning">
          <button class="reasoning__toggle" @click="m._showReasoning = !m._showReasoning">
            <span class="reasoning__chevron" :class="{ 'reasoning__chevron--open': m._showReasoning }">▶</span> 思考过程
          </button>
          <div class="reasoning__body" v-show="m._showReasoning" v-text="m.reasoning"></div>
        </div>
        <span class="msg__text" v-text="m.content"></span>
      </div>
    </div>

    <!-- Todo Panel -->
    <div class="todos" :class="{ 'todos--visible': showTodoPanel }" id="todo-panel">
      <div class="todos__head" id="todos-head">
        <span class="todos__title">任务列表</span>
        <span class="todos__badge" id="todos-badge">{{ todosState.filter(t => t.status === 'completed').length }}/{{ todosState.length }}</span>
        <span class="todos__summary" id="todos-summary">{{ todosState.find(t => t.status === 'in_progress')?.activeForm || todosState[todosState.length - 1]?.content || '' }}</span>
        <span class="todos__chev" style="margin-left:auto">▼</span>
        <span class="todos__dismiss" id="todos-dismiss" @click="todosDismissed = true; showTodoPanel = false">&times;</span>
      </div>
      <ul class="todos__list" id="todos-list">
        <li v-for="(t, i) in todosState" :key="i"
          class="todos__item"
          :class="{ 'todos__item--sub': t.level, 'todos__item--completed': t.status === 'completed', 'todos__item--in_progress': t.status === 'in_progress' }">
          <span class="todos__status"
            :class="{ 'todos__status--completed': t.status === 'completed', 'todos__status--in_progress': t.status === 'in_progress' }">
            {{ t.status === 'completed' ? '✓' : t.status === 'in_progress' ? '▶' : '○' }}
          </span>
          <span class="todos__text">{{ (t.status === 'in_progress' && t.activeForm) ? t.activeForm : t.content }}</span>
        </li>
      </ul>
    </div>

    <!-- Goal active bar -->
    <div class="goal-chip" v-if="goalActive" @click="toggleGoalMode()" style="max-width:760px;margin:0 auto 8px">
      <svg class="goal-chip__icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><circle cx="12" cy="12" r="6"/><line x1="12" y1="2" x2="12" y2="4"/><line x1="12" y1="20" x2="12" y2="22"/><line x1="2" y1="12" x2="4" y2="12"/><line x1="20" y1="12" x2="22" y2="12"/></svg>
      <span class="goal-chip__text" v-text="goalText"></span>
      <span class="goal-chip__close">&times;</span>
    </div>
  </section>

  <footer class="footer">
    <div class="toolbar">
      <button class="toolbar__btn" :class="{ 'toolbar__btn--ok': !bypassMode && !planMode }" id="btn-auto" title="自动模式" @click="setToolApprovalMode('auto'); setPlan(false)">
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><path d="M8 12l2 2 4-4"/></svg> 自动
      </button>
      <button class="toolbar__btn" :class="{ 'toolbar__btn--active': planMode }" id="btn-plan" title="计划模式" @click="setPlan(!planMode)">
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M2 3h6a4 4 0 0 1 4 4v14a3 3 0 0 0-3-3H2z"/><path d="M22 3h-6a4 4 0 0 0-4 4v14a3 3 0 0 1 3-3h7z"/></svg> 计划
      </button>
      <button class="toolbar__btn" :class="{ 'toolbar__btn--danger': bypassMode }" id="btn-bypass" title="YOLO 模式" @click="toggleYolo">
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z"/></svg> YOLO
      </button>
      <button class="toolbar__btn" :class="{ 'toolbar__btn--goal': goalMode && !goalActive, 'toolbar__btn--goal-active': goalActive }" id="btn-goal" title="目标模式" @click="toggleGoalMode">
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><circle cx="12" cy="12" r="6"/><line x1="12" y1="2" x2="12" y2="4"/><line x1="12" y1="20" x2="12" y2="22"/><line x1="2" y1="12" x2="4" y2="12"/><line x1="20" y1="12" x2="22" y2="12"/></svg> 目标
      </button>
      <div class="toolbar__sep"></div>
      <div class="status">
        <span class="status__dot" id="status-dot-footer" :class="{ 'status__dot--busy': running }"></span>
        <span id="status-text">{{ statusText }}</span>
      </div>
      <template v-if="pastedBlocks.filter(b => inputText.includes(b.label)).length > 0">
        <div style="display:flex;align-items:center;gap:4px;flex-shrink:0;max-width:500px">
          <div v-for="block in pastedBlocks.filter(b => inputText.includes(b.label))" :key="block.label" style="display:flex;align-items:center;gap:3px;padding:1px 6px;border:1px solid var(--border);border-radius:4px;background:var(--bg-2);font-size:10px;flex:1">
            <span style="color:var(--muted-2);overflow:hidden;text-overflow:ellipsis;white-space:nowrap;max-width:70px">{{ block.label }}</span>
            <button class="pasted-expand-btn" @click.stop="togglePastedBlock(block.label)" style="border:none;background:transparent;color:var(--accent);cursor:pointer;font-size:9px;padding:1px 3px;border-radius:2px">{{ openPastedLabels.includes(block.label) ? '收起' : '展开' }}</button>
            <button class="pasted-del-btn" @click.stop="removePastedBlock(block)" style="border:none;background:transparent;color:var(--danger);cursor:pointer;font-size:11px;width:14px;height:14px;border-radius:2px;display:flex;align-items:center;justify-content:center">×</button>
          </div>
        </div>
      </template>
      <div class="toolbar__spacer"></div>
      <div class="wf-bar" id="wf-bar" v-if="wfVisible">
        <div v-for="(s, i) in wfStages" :key="s.stage || i" class="wf-step" @click="setStage(s.stage)" title="切换到此阶段">
          <span class="wf-dot" :class="{ 'wf-dot--active': i === activeStageIdx, 'wf-dot--done': i < activeStageIdx }"></span>
          <span class="wf-label" :class="{ 'wf-label--active': i === activeStageIdx, 'wf-label--done': i < activeStageIdx }">{{ s.label || s.stage }}</span>
          <div class="wf-line" v-if="i < wfStages.length - 1" :class="{ 'wf-line--done': i < activeStageIdx }"></div>
        </div>
      </div>
      <div class="status" id="turn-info"></div>
    </div>
<template v-for="block in pastedBlocks.filter(b => openPastedLabels.includes(b.label) && inputText.includes(b.label))">
      <div style="padding:4px 28px;margin-bottom:2px;position:relative">
        <div class="preview-resize-handle" @mousedown="startPreviewResize($event)" style="height:10px;cursor:row-resize;position:relative;margin-bottom:-1px;display:flex;align-items:center;justify-content:center">
          <div style="height:2px;background:var(--border);border-radius:2px;flex:1;margin:0 20px;transition:all .15s"></div>
        </div>
        <div ref="previewRef" :style="{padding:'6px 10px',border:'1px solid var(--border)',borderRadius:'6px',background:'var(--bg)',fontSize:'11px',fontFamily:'var(--mono)',height:previewHeight+'px',overflowY:'auto',whiteSpace:'pre-wrap',wordBreak:'break-word'}">{{ block.text }}</div>
      </div>
    </template>
<div class="composer" id="composer" style="position:relative">
      <!-- Slash menu -->
      <div id="slash-menu-anchor" style="position:absolute;bottom:100%;left:0;right:0;z-index:50">
        <div class="slash-menu" id="slash-menu" v-if="slashOpen">
          <div class="slash-menu__head">
            <span>命令面板</span>
            <span class="slash-menu__query">/{{ inputText.slice(1).split(' ')[0] }}</span>
          </div>
          <div class="slash-menu__list" id="slash-menu-list">
            <template v-for="(c, i) in slashFiltered" :key="c.cmd">
              <div v-if="i === 0 || slashFiltered[i-1].group !== c.group" class="slash-menu__group">
                {{ { session: '会话', branch: '分支', model: '模型', agent: '代理', system: '系统', memory: '记忆', help: '帮助' }[c.group] || c.group }}
              </div>
              <button class="slash-menu__item" :class="{ 'slash-menu__item--active': i === slashIndex }"
                type="button" role="option" :aria-selected="i === slashIndex"
                @mouseenter="slashIndex = i"
                @click="inputText = '/' + c.cmd + ' '; closeSlashMenu(); nextTick(() => document.getElementById('in')?.focus())">
                <span class="slash-menu__name">/{{ c.cmd }}</span>
                <span class="slash-menu__desc">{{ c.desc }}</span>
                <span class="slash-menu__pill" :class="{ 'slash-menu__pill--danger': c.danger }">{{ c.danger ? '危险' : c.group }}</span>
                <span class="slash-menu__sig">{{ c.sig }}</span>
              </button>
            </template>
          </div>
          <div class="slash-menu__foot">
            <span>↑↓ 选择 · Enter 插入 · Esc 关闭</span>
            <span>{{ slashFiltered.length }}</span>
          </div>
        </div>
      </div>

      <div class="composer__input-row" style="display:flex;align-items:center;gap:7px;flex:1;min-width:0">
        <span class="composer__caret">›</span>
        <textarea v-model="inputText" class="composer__input" id="in"
          :placeholder="goalMode ? '描述你的目标...' : '给 Reasonix 发消息...  / 查看命令'"
          rows="1"
          @keydown="onInputKeydown"
          @input="onInput"
          @dragover.prevent
          @paste="handlePaste" @drop="handleFileDrop"></textarea>
      </div>
      <div class="composer-resize-handle" @mousedown="startComposerResize($event)" style="height:12px;cursor:row-resize;position:absolute;top:-6px;left:0;right:0;z-index:5;display:flex;align-items:center;justify-content:center">
        <div style="height:2px;background:var(--border);border-radius:2px;margin:0 28px;flex:1;transition:all .15s"></div>
      </div>
      <button class="composer__btn composer__btn--send" id="btn-send" title="发送 (Enter)" v-show="!running" @click="send">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" width="18" height="18"><line x1="12" y1="19" x2="12" y2="5"/><polyline points="5 12 12 5 19 12"/></svg>
      </button>
      <button class="composer__btn composer__btn--stop" id="btn-stop" title="取消 (Esc)" v-show="running" @click="doStop">
        <svg viewBox="0 0 24 24" fill="currentColor" width="18" height="18"><rect x="6" y="6" width="12" height="12" rx="2"/></svg>
      </button>
    </div>
  </footer>

  <!-- Rewind Picker -->
  <div class="rewind-overlay" v-if="showRewind" @click.self="showRewind = false">
    <div class="rewind-picker">
      <template v-if="rewindStage === 0">
        <div class="rewind-picker__head">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="1 4 1 10 7 10"/><path d="M3.51 15a9 9 0 1 0 2.13-9.36L1 10"/></svg>
          回退 — 选择轮次
        </div>
        <div class="rewind-picker__list">
          <div v-for="(cp, i) in rewindCheckpoints" :key="'cp-' + i + '-' + rewindKey"
            class="rewind-picker__item" :class="{ 'rewind-picker__item--active': i === rewindSelected }"
            @click="selectRewindCheckpoint(i); advanceRewindStage()">
            <span class="rewind-picker__turn">#{{ cp.turn }}</span>
            <span class="rewind-picker__prompt">{{ (cp.prompt || '').slice(0, 80) }}</span>
            <span class="rewind-picker__files">{{ cp.files || 0 }} 个文件</span>
          </div>
        </div>
        <div class="rewind-picker__foot">
          <span>j/k 或 ↑↓ 导航</span>
          <span>Enter 选择 · Esc 关闭</span>
        </div>
      </template>
      <template v-else>
        <div class="rewind-picker__head">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="1 4 1 10 7 10"/><path d="M3.51 15a9 9 0 1 0 2.13-9.36L1 10"/></svg>
          第 #{{ rewindCheckpoints[rewindSelected]?.turn }} 轮 — 选择操作
        </div>
        <div class="rewind-picker__scopes">
          <div v-for="(sc, i) in SCOPES" :key="'sc-' + i + '-' + rewindKey"
            class="rewind-picker__scope" :class="{ 'rewind-picker__scope--active': i === rewindScope }"
            @click="rewindScope = i; rewindKey++; applyRewind()">
            <span class="rewind-picker__scope-key">{{ sc.key }}</span>
            {{ sc.label }}
          </div>
        </div>
        <div class="rewind-picker__foot">
          <span>b/c/d/f/s/u 快捷键</span>
          <span>Enter 应用 · Esc 返回</span>
        </div>
      </template>
    </div>
  </div>
</template>

<style scoped>
.msg__text { white-space: pre-wrap; word-break: break-word; line-height: 1.65; }
.pasted-expand-btn:hover { background: var(--accent-soft) !important; }
.pasted-del-btn:hover { background: var(--danger-soft) !important; }
.preview-resize-handle:hover div, .composer-resize-handle:hover div { background: var(--accent) !important; height: 3px !important; margin: 0 10px !important; }

:deep(.card) { background: var(--card); border-radius: var(--radius-lg); overflow: hidden; font-size: 14px; box-shadow: var(--shadow-sm); margin: 8px auto; transition: border-color .18s ease; max-width: 760px; }
:deep(.card-head) { display: grid; grid-template-columns: auto minmax(0, 1fr) auto auto; align-items: center; gap: 8px; padding: 7px 12px; font-size: 13px; color: var(--fg-2); cursor: pointer; user-select: none; background: var(--card); transition: background .18s ease; }
:deep(.card-head:hover) { background: var(--card-hover); }
:deep(.card-head .ico) { width: 18px; height: 18px; border-radius: 5px; display: inline-flex; align-items: center; justify-content: center; background: var(--panel-2); color: var(--fg-2); flex-shrink: 0; font-size: 10px; }
:deep(.card-main) { display: grid; gap: 2px; min-width: 0; }
:deep(.card-title) { display: flex; align-items: center; gap: 7px; min-width: 0; }
:deep(.card-head .name) { color: var(--fg); font-weight: 500; font-size: 12.5px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
:deep(.card-head .subject) { font-family: var(--mono); font-size: 11.5px; color: var(--muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; min-width: 0; }
:deep(.card-meta) { display: flex; align-items: center; gap: 8px; min-width: 0; font-family: var(--mono); font-size: 10.5px; color: var(--muted-2); }
:deep(.card-badge) { display: inline-flex; align-items: center; height: 20px; padding: 0 7px; border-radius: 999px; border: 1px solid var(--border); font-size: 10.5px; color: var(--muted); white-space: nowrap; }
:deep(.card-actions) { display: flex; align-items: center; gap: 4px; }
:deep(.card-action) { display: inline-flex; align-items: center; justify-content: center; width: 24px; height: 24px; border: 1px solid var(--border); border-radius: 6px; background: var(--bg-2); color: var(--muted); }
:deep(.card-action:hover) { background: var(--panel); color: var(--fg); border-color: var(--border-strong); }
:deep(.card-head .chev) { color: var(--muted-2); transition: transform .15s; display: inline-flex; }
:deep(.card[data-open="false"] .card-head .chev) { transform: rotate(-90deg); }
:deep(.card-body) { border-top: 1px solid var(--border); background: var(--bg-2); padding: 8px 12px; font-family: var(--mono); font-size: 12px; color: var(--fg-2); white-space: pre-wrap; word-break: break-word; max-height: 240px; overflow-y: auto; line-height: 1.55; }
:deep(.card[data-tone="success"] .card-head .ico) { background: var(--success-soft); color: var(--success); }
:deep(.card[data-tone="danger"] .card-head .ico) { background: var(--danger-soft); color: var(--danger); }
:deep(.card[data-tone="accent"] .card-head .ico) { background: var(--accent-soft); color: var(--accent); }
:deep(.card[data-tone="success"] .card-badge) { background: var(--success-soft); border-color: transparent; color: var(--success); }
:deep(.card[data-tone="danger"] .card-badge) { background: var(--danger-soft); border-color: transparent; color: var(--danger); }
:deep(.card[data-tone="accent"] .card-badge) { background: var(--accent-soft); border-color: transparent; color: var(--accent); }
:deep(.card .err-body) { padding: 6px 12px; color: var(--danger); background: var(--danger-soft); font-family: var(--mono); font-size: 12px; white-space: pre-wrap; border-top: 1px solid var(--border); }
:deep(.spin) { animation: spin .9s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
:deep(.metric-strip) { display: flex; gap: 0; padding: 5px 0; border-top: 1px dashed var(--border); border-bottom: 1px dashed var(--border); margin: 8px auto; max-width: 760px; font-family: var(--mono); font-size: 11px; color: var(--muted); }
:deep(.metric-strip .item) { padding: 0 10px; display: flex; align-items: center; gap: 5px; border-right: 1px solid var(--border); }
:deep(.metric-strip .item:last-child) { border-right: none; }
:deep(.metric-strip .v) { color: var(--fg); }
:deep(.metric-strip .v.acc) { color: var(--accent); }
:deep(.metric-strip .v.ok) { color: var(--success); }
</style>
