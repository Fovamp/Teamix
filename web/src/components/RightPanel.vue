<script setup lang="ts">
import { ref, watch, onMounted, onUnmounted } from "vue"
import { api } from "../api"

const emit = defineEmits<{ (e: "open-projects"): void }>()

const showRp = ref(true)
const treeData = ref<any>(null)
const notifications = ref<any[]>([])
const notiCollapsed = ref(true)
const projectName = ref("项目文件")
const currentProject = ref("")
// AI 文件操作日志（文件树高亮 + 确认/取消，与 git 隔离）
const aiOps = ref<any[]>([])
const aiOpsCollapsed = ref(true)
let aiOpsTimer: any = null
// 文件树搜索过滤
const treeFilter = ref("")
// 右键菜单（文件树操作：新建/重命名/删除）
const ctxMenu = ref<{ x: number; y: number; path: string; isDir: boolean } | null>(null)

async function loadFileOps() {
  if (!currentProject.value) return
  try {
    aiOps.value = (await api.fileOps(currentProject.value)) || []
  } catch {}
}
function ackOp(id: string) {
  api.fileOpsAck(id).then(() => { loadFileOps(); loadFileTree() }).catch(() => {})
}
function undoOp(id: string) {
  api.fileOpsUndo(id).then((r: any) => {
    if (r && r.ok) { loadFileOps(); loadFileTree() } else { alert("撤销失败：快照不存在或已被处理")
    }
  }).catch(() => {})
}
function ackAllOp() {
  api.fileOpsAckAll(currentProject.value).then(() => { loadFileOps(); loadFileTree() }).catch(() => {})
}
// 操作计数（文件 + 目录递归聚合，供小圆点数字穿透）：{ path: {new, old} }
function opCounts(): Record<string, { n: number; o: number }> {
  const m: Record<string, { n: number; o: number }> = {}
  const bump = (p: string, isNew: boolean) => {
    const e = m[p] || { n: 0, o: 0 }
    if (isNew) e.n++; else e.o++
    m[p] = e
  }
  for (const op of aiOps.value) {
    bump(op.path, op.status === "new")
    // 目录穿透：每一级父路径都 +1
    const parts = (op.path || "").split("/")
    for (let i = 1; i < parts.length; i++) {
      bump(parts.slice(0, i).join("/"), op.status === "new")
    }
  }
  return m
}
// 操作备注（悬浮）：时间/会话/第几轮/解决什么问题
function opNote(op: any): string {
  const t = new Date(op.time).toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
  return ["时间: " + t, "会话: " + (op.session || "-"), "第 " + (op.turn || 0) + " 轮", "问题: " + (op.issue || "-")].join("\n")
}
// 文件树操作弹窗（页面风格，替代原生 prompt/confirm）
const treeDlg = ref<{ mode: string; path: string; isDir: boolean; value: string } | null>(null)
function askTreeOp(mode: string, path: string, isDir: boolean) {
  ctxMenu.value = null
  treeDlg.value = { mode, path, isDir, value: mode === "rename" ? path : "" }
}
async function confirmTreeDlg() {
  const d = treeDlg.value
  if (!d) return
  const project = currentProject.value
  if (!project) return
  const action = d.mode
  let name = d.value.trim()
  if (action === "mkdir" || action === "write") {
    if (!name) { alert("请输入路径"); return }
    name = (d.path ? d.path + "/" : "") + name
  }
  if (action === "rename" && !name) return
  treeDlg.value = null
  try {
    await api.fileTreeOps({ action: action === "write" ? "write" : action, project, path: d.path, name, content: "" })
    loadFileTree()
  } catch (e: any) {
    alert(e.message || "操作失败")
  }
}
function closeCtxMenu() { ctxMenu.value = null }

onMounted(async () => {
  try {
    treeData.value = await api.tree()
    updateProjectName()
  } catch {}
  try {
    const st = await api.status()
    if (st && st.selectedProject) {
      currentProject.value = st.selectedProject
      projectName.value = st.selectedProject
    }
  } catch {}
  window.addEventListener("notifications-update", onNotiUpdate as any)
  window.addEventListener("teamix-project-selected", reloadTree as any)
  const saved = localStorage.getItem("rp_noti_collapsed")
  if (saved !== null) notiCollapsed.value = saved === "true"
  loadNotis()
  // AI 操作日志轮询（3s；也可由 /events 的 fileop 事件触发，此处轮询兜底）
  loadFileOps()
  aiOpsTimer = setInterval(loadFileOps, 3000)

  // rp-resize-h drag for tree/noti split
  const resize = document.getElementById('rp-resize-h')
  const tree = document.querySelector('.right-panel__tree') as HTMLElement
  const noti = document.querySelector('.rp-noti') as HTMLElement
  if (resize && tree && noti) {
    let startY = 0, startTreeFlex = 3, startNotiFlex = 2
    const onPDown = (ev: PointerEvent) => {
      ev.preventDefault()
      resize.setPointerCapture(ev.pointerId)
      resize.classList.add('rp-resize-h--dragging')
      startY = ev.clientY
      startTreeFlex = parseFloat(tree.style.flex) || 3
      startNotiFlex = parseFloat(noti.style.flex) || 2
    }
    const onPMove = (ev: PointerEvent) => {
      if (!resize.classList.contains('rp-resize-h--dragging')) return
      const panel = resize.parentElement!
      const ph = panel.clientHeight
      if (ph < 50) return
      const dy = ev.clientY - startY
      const total = startTreeFlex + startNotiFlex
      const fpp = total / ph
      const ntf = Math.max(1, startTreeFlex + dy * fpp)
      const nnf = Math.max(0.5, total - ntf)
      if (ntf + nnf < 1) return
      tree.style.flex = String(ntf)
      noti.style.flex = String(nnf)
    }
    const onPUp = () => { resize.classList.remove('rp-resize-h--dragging') }
    resize.addEventListener('pointerdown', onPDown)
    resize.addEventListener('pointermove', onPMove)
    resize.addEventListener('pointerup', onPUp)
  }
})

watch(treeData, () => { setTimeout(loadFileTree, 50) })

onUnmounted(() => {
  window.removeEventListener("notifications-update", onNotiUpdate as any)
  window.removeEventListener("teamix-project-selected", reloadTree as any)
  if (aiOpsTimer) clearInterval(aiOpsTimer)
})

async function reloadTree() {
  try {
    treeData.value = await api.tree()
    updateProjectName()
    const st = await api.status().catch(() => null)
    if (st && st.selectedProject) {
      currentProject.value = st.selectedProject
      projectName.value = st.selectedProject
    }
    loadNotis()
    loadFileOps() // 切项目后刷新操作日志
  } catch {}
}

function onNotiUpdate(e: CustomEvent) {
  if (Array.isArray(e.detail)) notifications.value = e.detail
}

// 拉取个人通知（通知统一存 <user>.json）。
async function loadNotis() {
  try {
    notifications.value = await api.notifications()
  } catch {}
}

function updateProjectName() {
  if (treeData.value && treeData.value[0]?.path) {
    const parts = treeData.value[0].path.replace(/\\\\/g, '/').split('/')
    projectName.value = parts.slice(-2).join('/')
  }
}

function toggleNoti() {
  notiCollapsed.value = !notiCollapsed.value
  localStorage.setItem("rp_noti_collapsed", String(notiCollapsed.value))
}

function notiBadgeClass() {
  const unread = notifications.value.filter((n: any) => !n.read).length
  return unread === 0 ? "rp-noti__badge rp-noti__badge--empty" : "rp-noti__badge"
}

function markRead(n: any) {
  if (!n.read && n.id) {
    n.read = true
    const t = localStorage.getItem("teamix_token")
    if (!t) return
    fetch("/teamix/notifications/read?token=" + encodeURIComponent(t), {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ id: n.id, project: n.project || currentProject.value || "" })
    }).catch(() => {})
  }
}

// ── File tree: recursive DOM approach matching original rn() + tog() ──
function loadFileTree() {
  const el = document.getElementById('rp-tree')
  if (!el || !treeData.value) return
  if (treeData.value.empty !== undefined) {
    if (treeData.value.reason === "noProject") {
      el.innerHTML = '<div style="padding:12px;color:var(--muted-2);font-size:12px;text-align:center">请先选择项目</div>'
    } else {
      el.innerHTML = '<div style="padding:12px;color:var(--muted-2);font-size:12px;text-align:center">项目目录为空</div>'
    }
    return
  }
  if (treeData.value.length === 0) {
    el.innerHTML = '<div style="padding:12px;color:var(--muted-2);font-size:12px;text-align:center">目录为空</div>'
    return
  }
  el.innerHTML = ''
  // 搜索过滤：文件名/路径匹配（默认）或内容搜索（前缀 内容:）
  let nodes = treeData.value
  const f = treeFilter.value.trim()
  if (f) {
    if (f.startsWith(':')) {
      const q = f.slice(1).trim()
      if (q) {
        // 内容搜索：后端 grep → 只显示命中文件及其祖先目录
        api.fileTreeSearch(currentProject.value, q).then((res: any) => {
          const hits = new Set((res && res.hits) || [])
          if (hits.size === 0) {
            el.innerHTML = '<div style="padding:12px;color:var(--muted-2);font-size:12px;text-align:center">无内容命中</div>'
            return
          }
          const hitPath = (p: string): boolean => {
            // 节点路径是某命中的前缀（目录）或等于命中（文件）
            for (const h of hits) {
              if (h === p || h.startsWith(p + '/')) return true
            }
            return false
          }
          const prune = (list: any[]): any[] => {
            const out: any[] = []
            for (const n of list) {
              const p = n.path || n.name || ''
              if (hitPath(p)) out.push(n)
            }
            return out
          }
          const pruned = prune(treeData.value)
          if (pruned.length === 0) {
            el.innerHTML = '<div style="padding:12px;color:var(--muted-2);font-size:12px;text-align:center">无内容命中</div>'
            return
          }
          el.innerHTML = ''
          pruned.forEach((n: any) => { el.appendChild(rn(n, 0)) })
        }).catch(() => {})
      }
      return
    }
    const q = f.toLowerCase()
    const match = (n: any): boolean => {
      const p = (n.path || n.name || '').toLowerCase()
      if (p.indexOf(q) >= 0) return true
      if (n.children) return n.children.some(match)
      return false
    }
    const prune = (list: any[]): any[] => {
      const out: any[] = []
      for (const n of list) {
        if (match(n)) out.push(n)
      }
      return out
    }
    nodes = prune(nodes)
  }
  nodes.forEach((n: any) => { el.appendChild(rn(n, 0)) })
}

function rn(n: any, d: number): HTMLElement {
  const counts = opCounts()
  const wrap = document.createElement('div')
  const _rp = n.path || n.name || ''
  wrap.setAttribute('data-p', _rp)
  // 右键菜单（文件树操作：新建/重命名/删除）
  wrap.addEventListener('contextmenu', (e) => {
    e.preventDefault()
    ctxMenu.value = { x: e.clientX, y: e.clientY, path: _rp, isDir: !!n.isDir }
  })
  if (n.isDir) {
    const header = document.createElement('div')
    header.className = 'rp-item rp-item--dir'
    // AI 操作穿透：目录聚合小圆点数字
    const cnt = counts[n.path || n.name]
    if (cnt && (cnt.n + cnt.o) > 0) {
      header.classList.add(cnt.n > 0 ? 'rp-item--ai-new' : 'rp-item--ai-old')
      const dot = document.createElement('span')
      dot.className = 'rp-dot'
      dot.textContent = String(cnt.n + cnt.o)
      header.appendChild(dot)
    }
    const hasKids = n.children && n.children.length > 0
    const a = document.createElement('span')
    a.className = 'rp-a'
    a.textContent = hasKids ? '\u25b6' : ''
    header.appendChild(a)
    const l = document.createElement('span')
    l.className = 'rp-l'
    l.textContent = n.name
    header.appendChild(l)
    wrap.appendChild(header)
    wrap.draggable = true
    wrap.title = _rp
    wrap.ondragstart = (e) => {
      const el = (e.target as HTMLElement).closest('[data-p]')
      const p = el ? el.getAttribute('data-p') : _rp
      ;(window as any)._dragPath = '@' + p + '/'
      e.dataTransfer!.setData('text/plain', (window as any)._dragPath)
      e.dataTransfer!.effectAllowed = 'copy'
    }
    const c = document.createElement('div')
    c.className = 'rp-c'
    c.style.display = 'none'
    wrap.appendChild(c)
    if (hasKids) {
      n.children.forEach((ch: any) => { c.appendChild(rn(ch, d + 1)) })
      function tog() {
        if (a.textContent === '\u25bc') {
          a.textContent = '\u25b6'
          c.style.display = 'none'
        } else {
          a.textContent = '\u25bc'
          c.style.display = ''
          const kids = c.querySelectorAll('.rp-c')
          for (let i = 0; i < kids.length; i++) { (kids[i] as HTMLElement).style.display = 'none' }
          const arr = c.querySelectorAll('.rp-a')
          for (let i = 0; i < arr.length; i++) { if (arr[i].textContent === '\u25bc') arr[i].textContent = '\u25b6' }
        }
      }
      a.onclick = (e) => { e.stopPropagation(); tog() }
      header.onclick = () => { tog() }
    }
  } else {
    wrap.className = 'rp-item'
    // AI 操作高亮：new 明显（亮黄）/ old 淡色 + 小圆点数字
    const cnt = counts[n.path || n.name]
    if (cnt && (cnt.n + cnt.o) > 0) {
      wrap.classList.add(cnt.n > 0 ? 'rp-item--ai-new' : 'rp-item--ai-old')
      const dot = document.createElement('span')
      dot.className = 'rp-dot'
      dot.textContent = String(cnt.n + cnt.o)
      dot.title = 'AI 改动 ' + (cnt.n + cnt.o) + ' 次未确认'
      wrap.appendChild(dot)
    }
    wrap.draggable = true
    wrap.title = _rp
    wrap.ondragstart = (e) => {
      const el = (e.target as HTMLElement).closest('[data-p]')
      const p = el ? el.getAttribute('data-p') : _rp
      // agent 工作区 = 用户根，拖拽引用带项目前缀（树路径相对项目根）。
      ;(window as any)._dragPath = '@' + (currentProject.value ? currentProject.value + '/' : '') + p
      e.dataTransfer!.setData('text/plain', (window as any)._dragPath)
      e.dataTransfer!.effectAllowed = 'copy'
    }
    const s = document.createElement('span')
    s.style.cssText = 'width:14px;display:inline-block;flex-shrink:0'
    wrap.appendChild(s)
    const l = document.createElement('span')
    l.className = 'rp-l'
    l.textContent = n.name
    wrap.appendChild(l)
    wrap.onclick = () => { openFilePreview(_rp) }
  }
  return wrap
}

function openFilePreview(path: string) {
  if (!path) return
  let p = document.getElementById('preview-panel')
  if (!p) {
    p = document.createElement('div')
    p.id = 'preview-panel'
    p.style.cssText = 'position:fixed;right:0;top:0;bottom:0;width:400px;background:var(--panel);border-left:2px solid var(--accent);z-index:50;display:flex;flex-direction:column;box-shadow:var(--shadow-lg);animation:msg-in .2s ease;min-width:200px'
    p.innerHTML = '<div id="pv-resize" style="position:absolute;left:-4px;top:0;bottom:0;width:8px;cursor:col-resize;z-index:5"></div>' +
      '<div style="display:flex;align-items:center;justify-content:space-between;padding:8px 12px;border-bottom:1px solid var(--border);font-size:13px;font-weight:500"><span id="pv-title" style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap"></span>' +
      '<span style="display:flex;gap:8px;align-items:center"><button id="pv-save" style="display:none;font-size:11px;padding:2px 10px;border:none;border-radius:6px;background:var(--accent);color:#000;cursor:pointer">保存</button>' +
      '<span id="pv-close" style="cursor:pointer;font-size:18px;color:var(--muted-2);line-height:1">&times;</span></span></div>' +
      '<div id="pv-wrap" style="flex:1;overflow:auto;padding:12px;box-sizing:border-box;background:var(--bg)">' +
      '<textarea id="pv-body" spellcheck="false" style="width:100%;min-height:100%;box-sizing:border-box;font-family:var(--mono);font-size:12px;line-height:1.5;color:var(--fg-2);background:transparent;border:none;resize:none;outline:none;overflow:hidden;white-space:pre;tab-size:2;display:block"></textarea></div>' +
      '<div id="pv-status" style="display:none;padding:4px 12px;font-size:11px;color:var(--muted-2);border-top:1px solid var(--border)"></div>'
    document.body.appendChild(p)
    document.getElementById('pv-close')!.onclick = () => { p!.style.display = 'none' }
    const rv = document.getElementById('pv-resize')!
    let startX = 0, startW = 0
    rv.addEventListener('mousedown', function(e) {
      startX = e.clientX; startW = p!.offsetWidth
      document.body.style.cursor = 'col-resize'; document.body.style.userSelect = 'none'
      function mm(ev: MouseEvent) { const w = Math.max(200, Math.min(800, startW + startX - ev.clientX)); p!.style.width = w + 'px' }
      function mu() { document.body.style.cursor = ''; document.body.style.userSelect = ''; document.removeEventListener('mousemove', mm); document.removeEventListener('mouseup', mu) }
      document.addEventListener('mousemove', mm)
      document.addEventListener('mouseup', mu)
      e.preventDefault()
    })
  }
  p.style.display = 'flex'
  document.getElementById('pv-title')!.textContent = path
  const body = document.getElementById('pv-body') as HTMLTextAreaElement
  const saveBtn = document.getElementById('pv-save') as HTMLElement
  const status = document.getElementById('pv-status') as HTMLElement
  body.value = 'Loading...'
  body.readOnly = true
  saveBtn.style.display = 'none'
  status.style.display = 'none'
  // textarea 高度自适应内容（滚动交给外层 #pv-wrap，滚动条光标为默认而非文字光标）
  const autosize = () => {
    body.style.height = 'auto'
    body.style.height = body.scrollHeight + 'px'
  }
  body.oninput = () => { if (!body.readOnly) autosize() }
  window.setTimeout(autosize, 0)
  const token = localStorage.getItem('teamix_token')
  const url = '/teamix/file?path=' + encodeURIComponent(path) + (token ? '&token=' + encodeURIComponent(token) : '')
  fetch(url)
    .then(async r => {
      const txt = await r.text()
      let data: any = null
      try { data = JSON.parse(txt) } catch { /* 非 JSON 响应体（如 4xx 纯文本） */ }
      if (!r.ok || !data) throw new Error('HTTP ' + r.status + (txt ? ' · ' + txt.slice(0, 160) : ''))
      body.value = data.body || 'Empty file'
      const isBinary = (data.body || '').indexOf('\u0000') >= 0
      body.readOnly = isBinary || !!data.truncated
      // 可编辑（文本且未截断）→ 显示保存按钮；二进制/大文件只读
      if (isBinary) {
        status.style.display = 'block'
        status.textContent = '⚠ 疑似二进制文件，仅只读'
      } else if (data.truncated) {
        status.style.display = 'block'
        status.textContent = '⚠ 文件较大已截断显示，为避免丢失内容已禁用编辑'
      } else {
        saveBtn.style.display = 'inline-block'
      }
    })
    .catch((e: Error) => { body.value = 'Error loading file: ' + e.message })
  // 保存（写回项目文件；用户操作，不在 AI 回合内，不进操作日志）
  saveBtn.onclick = async () => {
    saveBtn.textContent = '保存中...'
    try {
      await api.fileTreeOps({ action: 'write', project: currentProject.value, path, content: body.value })
      status.style.display = 'block'
      status.textContent = '✓ 已保存 ' + new Date().toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })
      saveBtn.textContent = '保存'
      loadFileTree()
    } catch (e: any) {
      status.style.display = 'block'
      status.textContent = '保存失败: ' + (e.message || '')
      saveBtn.textContent = '保存'
    }
  }
  body.onkeydown = (e) => {
    if ((e.ctrlKey || e.metaKey) && e.key === 's') { e.preventDefault(); saveBtn.click() }
  }
}
</script>

<template>
  <aside class="right-panel" v-if="showRp" id="right-panel">
    <div class="right-panel__title" id="rp-project-name" style="cursor:pointer" title="点击选择项目" @click="emit('open-projects')">
      <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="vertical-align:-1px;margin-right:6px"><path d="M3 7v10l7 4V11z"/><path d="M3 7l7-4 7 4-7 4z"/><path d="M17 7v10"/><path d="M10 11v10"/><path d="M17 11h4v6h-4"/></svg>
      {{ currentProject || "选择项目" }}
    </div>
    <!-- 文件搜索 -->
    <div class="rp-search" id="rp-search">
      <input v-model="treeFilter" @input="loadFileTree" placeholder=":关键词 搜内容 · 其他为文件名" spellcheck="false" />
      <span v-if="treeFilter" class="rp-search__clear" @click="treeFilter = ''; loadFileTree()">&times;</span>
    </div>
    <div class="right-panel__tree" id="rp-tree" style="flex:3;min-height:80px;padding:4px 0;overflow-y:auto;font-size:12px">
      <div v-if="!treeData" style="padding:16px;font-size:12px;color:var(--muted-2)">加载文件树...</div>
    </div>
    <div class="rp-resize-h" id="rp-resize-h"></div>
    <!-- AI 操作日志面板（文件树高亮配套：agent 修改的文件可确认/取消，与 git 隔离） -->
    <div v-if="currentProject && aiOps.length > 0" class="rp-aiops" id="rp-aiops">
      <div class="rp-aiops__head" @click="aiOpsCollapsed = !aiOpsCollapsed">
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline :points="aiOpsCollapsed ? '9 6 15 12 9 18' : '6 9 12 15 18 9'"/></svg>
        <span class="rp-aiops__title">AI 操作 {{ aiOps.length }} 项未确认</span>
        <span class="rp-aiops__ackall" @click.stop="ackAllOp()">全部确认</span>
      </div>
      <div v-if="!aiOpsCollapsed" class="rp-aiops__list">
        <div v-for="op in aiOps" :key="op.id" class="rp-aiops__row" :class="op.status === 'new' ? 'rp-aiops__row--new' : 'rp-aiops__row--old'">
          <div class="rp-aiops__meta" :title="opNote(op)">
            <code class="rp-aiops__path">{{ op.path }}</code>
            <span class="rp-aiops__time">{{ op.kind }} · +{{ op.added }}/-{{ op.removed }} · {{ new Date(op.time).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' }) }}</span>
            <span class="rp-aiops__note">{{ op.status === 'new' ? '本轮新改动' : '已确认过（新消息默认同意）' }}</span>
          </div>
          <div class="rp-aiops__btns">
            <button class="rp-aiops__btn rp-aiops__btn--ack" @click="ackOp(op.id)">确认</button>
            <button class="rp-aiops__btn rp-aiops__btn--undo" @click="undoOp(op.id)">取消</button>
          </div>
        </div>
      </div>
    </div>
    <div class="rp-noti" id="rp-noti" :class="{ 'rp-noti--collapsed': notiCollapsed }" style="flex:2;min-height:60px">
      <div class="rp-noti__head" id="rp-noti-head" @click="toggleNoti">
        <svg class="rp-noti__head-arrow" width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="6 9 12 15 18 9"/></svg>
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9"/><path d="M13.73 21a2 2 0 0 1-3.46 0"/></svg>
        通知
        <span :class="notiBadgeClass()" id="rp-noti-badge">{{ notifications.filter((n: any) => !n.read).length || '0' }}</span>
      </div>
      <div class="rp-noti__list" id="rp-noti-list" :class="{ 'rp-noti__list--open': !notiCollapsed }">
        <div v-if="notifications.length === 0" class="rp-noti__empty">暂无通知</div>
        <div v-for="(n, i) in notifications" :key="n.id || i"
          class="rp-noti__item" :class="{ 'rp-noti__item--read': n.read }"
          @click="markRead(n)">
          <div class="rp-noti__from">
            <span class="dot"></span>
            {{ n.fromUser || '系统' }}
            <span class="rp-noti__time">{{ new Date(n.time || n.createdAt).toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }) }}</span>
          </div>
          <div class="rp-noti__msg">{{ n.message }}
            <span v-if="n.project" class="rp-noti__proj">{{ n.project }}</span>
          </div>
          <span v-if="n.fileChanged" class="rp-noti__file">{{ n.fileChanged }}</span>
        </div>
      </div>
    </div>
  </aside>
  <!-- 文件树右键菜单 -->
  <div v-if="ctxMenu" class="rp-ctxmenu" :style="{ left: ctxMenu.x + 'px', top: ctxMenu.y + 'px' }" @click.stop @contextmenu.prevent>
    <div v-if="ctxMenu.isDir" class="rp-ctxmenu__item" @click="askTreeOp('mkdir', ctxMenu.path, true)">新建目录</div>
    <div v-if="ctxMenu.isDir" class="rp-ctxmenu__item" @click="askTreeOp('write', ctxMenu.path, true)">新建文件</div>
    <div class="rp-ctxmenu__item" @click="askTreeOp('rename', ctxMenu.path, ctxMenu.isDir)">重命名</div>
    <div class="rp-ctxmenu__item rp-ctxmenu__item--danger" @click="askTreeOp('delete', ctxMenu.path, ctxMenu.isDir)">删除</div>
  </div>
  <div v-if="ctxMenu" class="rp-ctxmenu-mask" @click="closeCtxMenu" @contextmenu.prevent="closeCtxMenu"></div>
  <!-- 文件树操作弹窗（重命名/新建/删除，页面风格） -->
  <div v-if="treeDlg" class="modal-overlay" style="display:flex;z-index:320" @click.self="treeDlg = null">
    <div class="modal" style="width:420px">
      <div class="modal__head"><span>{{ treeDlg.mode === 'delete' ? '删除' : treeDlg.mode === 'rename' ? '重命名' : treeDlg.mode === 'mkdir' ? '新建目录' : '新建文件' }}</span><span class="modal__close" @click="treeDlg = null">&times;</span></div>
      <div class="modal__body">
        <p v-if="treeDlg.mode === 'delete'" style="margin:0;font-size:13px;line-height:1.6">确定删除 <code>{{ treeDlg.path }}</code> ？此操作不可恢复。</p>
        <template v-else>
          <label style="display:block;font-size:11px;color:var(--muted-2);margin-bottom:4px">{{ treeDlg.mode === 'rename' ? '新路径（相对项目根）' : '名称（相对项目根，如 src/utils 或 x.ts）' }}</label>
          <input v-model="treeDlg.value" class="rp-dlg-input" @keydown.enter="confirmTreeDlg" :placeholder="treeDlg.mode === 'rename' ? 'src/newName.ts' : 'src/utils'" spellcheck="false" />
        </template>
      </div>
      <div style="display:flex;gap:8px;justify-content:flex-end;padding:10px 14px;border-top:1px solid var(--border)">
        <button class="rp-dlg-btn" @click="treeDlg = null">取消</button>
        <button class="rp-dlg-btn rp-dlg-btn--primary" @click="confirmTreeDlg">{{ treeDlg.mode === 'delete' ? '删除' : '确定' }}</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.rp-item { padding-top: 2px; padding-bottom: 2px; cursor: default; display: flex; gap: 4px; align-items: center; }
.rp-item:hover { background: var(--card-hover); }
.rp-item--dir { cursor: pointer; }
/* AI 操作高亮：new 明显（亮黄）/ old 淡色 + 小圆点数字穿透 */
.rp-item--ai-new { background: rgba(255, 193, 7, .16); border-left: 2px solid #ffc107; }
.rp-item--ai-new:hover { background: rgba(255, 193, 7, .22); }
.rp-item--ai-old { background: rgba(255, 193, 7, .05); border-left: 2px solid rgba(255, 193, 7, .4); }
.rp-item--ai-old:hover { background: rgba(255, 193, 7, .10); }
.rp-dot {
  font-size: 9px; min-width: 15px; height: 15px; padding: 0 3px; border-radius: 8px;
  background: #ffc107; color: #000; font-weight: 700; display: inline-flex; align-items: center;
  justify-content: center; margin-left: auto; flex-shrink: 0; line-height: 1; box-sizing: border-box;
}
.rp-item--ai-old .rp-dot { background: rgba(255, 193, 7, .45); color: #6b5b1a; }
/* 文件搜索 */
.rp-search { position: relative; padding: 4px 8px; border-bottom: 1px solid var(--border); }
.rp-search input {
  width: 100%; box-sizing: border-box; padding: 4px 20px 4px 8px; font-size: 11px;
  border: 1px solid var(--border); border-radius: 6px; background: var(--bg); color: var(--fg); outline: none;
}
.rp-search input:focus { border-color: var(--accent); }
.rp-search__clear { position: absolute; right: 14px; top: 50%; transform: translateY(-50%); cursor: pointer; color: var(--muted-2); font-size: 14px; }
/* AI 操作面板 */
.rp-aiops { border-bottom: 1px solid var(--border); background: var(--bg-2); }
.rp-aiops__head {
  display: flex; align-items: center; gap: 6px; padding: 5px 8px; cursor: pointer;
  font-size: 11px; color: var(--fg-2); border-bottom: 1px solid var(--border);
}
.rp-aiops__head svg { flex-shrink: 0; }
.rp-aiops__title { font-weight: 600; color: #ffc107; }
.rp-aiops__ackall { margin-left: auto; font-size: 10px; color: var(--accent); cursor: pointer; flex-shrink: 0; }
.rp-aiops__ackall:hover { text-decoration: underline; }
.rp-aiops__list { max-height: 160px; overflow-y: auto; }
.rp-aiops__row {
  display: flex; align-items: center; gap: 8px; padding: 4px 8px; font-size: 11px;
  border-bottom: 1px solid var(--border); }
.rp-aiops__row:last-child { border-bottom: none; }
.rp-aiops__row--new { background: rgba(255, 193, 7, .08); }
.rp-aiops__row--old { opacity: .72; }
.rp-aiops__meta { flex: 1; min-width: 0; }
.rp-aiops__path { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 11px; color: var(--fg); }
.rp-aiops__time { font-size: 10px; color: var(--muted-2); }
.rp-aiops__note { font-size: 10px; color: var(--muted-2); margin-left: 4px; }
.rp-aiops__btns { display: flex; gap: 4px; flex-shrink: 0; }
.rp-aiops__btn { font-size: 10px; padding: 1px 8px; border-radius: 10px; border: 1px solid var(--border); background: var(--bg); cursor: pointer; }
.rp-aiops__btn--ack { color: #4caf50; border-color: rgba(76,175,80,.4); }
.rp-aiops__btn--undo { color: #f44336; border-color: rgba(244,67,54,.4); }
/* 右键菜单 */
.rp-ctxmenu {
  position: fixed; z-index: 300; min-width: 120px; background: var(--panel-2);
  border: 1px solid var(--border); border-radius: 8px; padding: 4px; box-shadow: var(--shadow-lg);
}
.rp-ctxmenu__item { padding: 6px 12px; font-size: 12px; cursor: pointer; border-radius: 5px; color: var(--fg-2); }
.rp-ctxmenu__item:hover { background: var(--bg-2); color: var(--fg); }
.rp-ctxmenu__item--danger { color: #f44336; }
.rp-ctxmenu-mask { position: fixed; inset: 0; z-index: 299; }
/* 文件树操作弹窗 */
.rp-dlg-input {
  width: 100%; box-sizing: border-box; padding: 7px 10px; font-size: 12px;
  border: 1px solid var(--border); border-radius: 6px; background: var(--bg); color: var(--fg);
  outline: none; font-family: var(--mono);
}
.rp-dlg-input:focus { border-color: var(--accent); }
.rp-dlg-btn { padding: 6px 16px; font-size: 12px; border: 1px solid var(--border); border-radius: 6px; background: var(--bg); color: var(--fg-2); cursor: pointer; }
.rp-dlg-btn:hover { background: var(--bg-2); color: var(--fg); }
.rp-dlg-btn--primary { border: none; background: var(--accent); color: #000; font-weight: 600; }
.rp-dlg-btn--primary:hover { background: var(--accent-strong); color: #000; }
/* 编辑面板滚动条（外层滚动容器，光标默认非文字） */
#pv-wrap { scrollbar-width: thin; scrollbar-color: var(--border) transparent; }
#pv-wrap::-webkit-scrollbar { width: 9px; height: 9px; }
#pv-wrap::-webkit-scrollbar-track { background: transparent; }
#pv-wrap::-webkit-scrollbar-thumb { background: var(--border); border-radius: 5px; }
#pv-wrap::-webkit-scrollbar-thumb:hover { background: var(--muted-2); }
#pv-wrap::-webkit-scrollbar-corner { background: transparent; }
.rp-a { width: 14px; flex-shrink: 0; cursor: pointer; font-size: 10px; color: var(--muted-2); }
.rp-l { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.rp-noti__proj { margin-left: 6px; font-size: 10px; padding: 1px 6px; border-radius: 99px; background: var(--accent-soft); color: var(--accent); vertical-align: middle; }
</style>
