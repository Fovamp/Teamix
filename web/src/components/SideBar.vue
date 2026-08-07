<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from "vue"
import { api } from "../api"
import { useToast } from "../composables/useToast"
const { toast } = useToast()
const emit = defineEmits<{ (e: "stats"): void; (e: "branches"): void; (e: "models"): void; (e: "workflows"): void; (e: "settings"): void; (e: "summaries"): void }>()
const sessions = ref<any[]>([])
const userName = ref("")
const token = ref(localStorage.getItem("teamix_token") || "")
const running = ref(false)
const ctxFill = ref(0)
const ctxUsed = ref("0 tok")
const ctxWindow = ref("0 tok")
const ctxBarColor = ref("var(--accent)")
const statusModel = ref("-")
const smCache = ref("—")
const smCost = ref("—")
const smBalance = ref("—")
const deleteName = ref("")
const showDelete = ref(false)
const sessionFilter = ref("")

const hasVisibleHistory = ref(false)
const isArchitect = ref(false)
const checkpointCount = ref(0)

let _cpCache = 0
function hasCheckpoints(): boolean { return _cpCache > 0 }
function refreshCheckpoints() {
  const t = localStorage.getItem('teamix_token')
  if (!t) return
  fetch('/checkpoints?token=' + encodeURIComponent(t))
    .then(r => r.json()).then(cps => { _cpCache = Array.isArray(cps) ? cps.length : 0 })
    .catch(() => {})
}

onMounted(() => {
  refreshCheckpoints()
  setInterval(refreshCheckpoints, 30000)
  // 点击别处关闭会话右键菜单（菜单自身 @mousedown.stop 阻止冒泡）
  document.addEventListener('mousedown', closeCtx)
  window.addEventListener('status-update', (e) => {
    const s = (e as CustomEvent).detail;
    checkpointCount.value = s.checkpointCount || 0;
    if (s.hasVisibleHistory !== undefined) hasVisibleHistory.value = s.hasVisibleHistory;
  });
  window.addEventListener('hasVisibleHistory-changed', ((e: CustomEvent) => {
    hasVisibleHistory.value = e.detail;
  }) as any);
  // Sidebar vertical resize: nav area vs sessions area
  const resizeH = document.getElementById('sidebar-resize-h')
  const nav = document.querySelector('.sidebar__nav') as HTMLElement
  if (resizeH && nav) {
    let startY = 0, startNavH = 0
    const onStart = (e: MouseEvent) => {
      startY = e.clientY
      startNavH = nav.offsetHeight
      resizeH.classList.add('sidebar__resize-h--active')
      document.body.style.cursor = 'row-resize'
      document.body.style.userSelect = 'none'
      e.preventDefault()
    }
    const onMove = (e: MouseEvent) => {
      if (startY === 0) return
      const dy = e.clientY - startY
      const sidebarH = document.querySelector('.sidebar')!.offsetHeight
      const newNavH = Math.max(240, Math.min(sidebarH - 120, startNavH + dy))
      nav.style.flex = '0 0 ' + newNavH + 'px'
    }
    const onEnd = () => {
      if (startY === 0) return
      resizeH.classList.remove('sidebar__resize-h--active')
      document.body.style.cursor = ''
      document.body.style.userSelect = ''
      startY = 0
    }
    resizeH.addEventListener('mousedown', onStart)
    document.addEventListener('mousemove', onMove)
    document.addEventListener('mouseup', onEnd)
  }
})

const filteredSessions = computed(() => {
  const q = sessionFilter.value.trim().toLowerCase()
  if (!q) return sessions.value
  return sessions.value.filter((s: any) => (s.title || s.name || "").toLowerCase().includes(q))
})

onMounted(async () => {
  try { const r = await api.userRole(); userName.value = r.user; isArchitect.value = r.role === 'architect' } catch {}
  try {
    const s = await api.status()
    if (s) updateStatusUI(s)
  } catch {}
  try { sessions.value = await api.sessions() } catch {}

  window.addEventListener("status-update", onStatusUpdate as any)
  window.addEventListener("sessions-update", onSessionsUpdate as any)
})

onUnmounted(() => {
  window.removeEventListener("status-update", onStatusUpdate as any)
  window.removeEventListener("sessions-update", onSessionsUpdate as any)
})

function onStatusUpdate(e: CustomEvent) {
  updateStatusUI(e.detail)
}
function updateStatusUI(s: any) {
  if (s.label) statusModel.value = s.label
  if (s.running !== undefined) running.value = s.running
  if (s.window) {
    const pct = Math.min(100, Math.round(s.used / s.window * 100))
    ctxFill.value = pct
    ctxUsed.value = fmtTok(s.used) + " tok"
    ctxWindow.value = fmtTok(s.window) + " tok"
    ctxBarColor.value = pct > 95 ? "var(--danger)" : pct > 85 ? "var(--warning)" : "var(--accent)"
  }
  const cacheTotal = (s.cacheHit || 0) + (s.cacheMiss || 0)
  smCache.value = cacheTotal > 0 ? Math.round((s.cacheHit || 0) / cacheTotal * 100) + "%" : "—"
  if (typeof s.totalCost === 'number' && s.totalCost > 0) {
    smCost.value = "$" + s.totalCost.toFixed(4)
  }
  if (s.balance) {
    smBalance.value = s.balance.display || "——"
  }
}
function onSessionsUpdate(e: CustomEvent) {
  if (Array.isArray(e.detail)) sessions.value = e.detail
}
function fmtTok(n: number) { return n >= 1000 ? (n / 1000).toFixed(1).replace(/\.0$/, '') + 'k' : String(n) }

async function newS() {
  console.log("newS clicked")
  window.dispatchEvent(new Event("new-session-requested"))
}
async function compact() {
  if (!hasVisibleHistory.value) return
  try { await api.compact() } catch {}
}
async function rewind() {
  if (!hasVisibleHistory.value) return
  window.dispatchEvent(new Event('open-rewind-picker'))
}
function lgout() { api.logout(); location.reload() }
const deleteMode = ref<'one' | 'others' | 'all'>('one')
const ctxMenu = ref<{ x: number; y: number; session: any } | null>(null)
function openCtx(e: MouseEvent, s: any) {
  ctxMenu.value = { x: e.clientX, y: e.clientY, session: s }
}
function closeCtx() { ctxMenu.value = null }
function confirmArchive(s: any) {
  deleteMode.value = 'one'
  deleteName.value = s.name || ''
  showDelete.value = true
  ctxMenu.value = null
}
function askArchiveOthers() {
  deleteMode.value = 'others'
  deleteName.value = ''
  showDelete.value = true
  ctxMenu.value = null
}
function askArchiveAll() {
  deleteMode.value = 'all'
  deleteName.value = ''
  showDelete.value = true
  ctxMenu.value = null
}
async function doArchive() {
  const name = deleteName.value
  try {
    if (deleteMode.value === 'one') {
      if (!name) return
      await api.archiveSession(name)
    } else {
      await api.archiveSessions(deleteMode.value)
    }
  } catch (err) {
    console.error('archiveSession error:', err)
    toast(deleteMode.value === 'one' ? '归档会话失败' : '批量归档会话失败，请重试')
  }
  showDelete.value = false
  try {
    sessions.value = await api.sessions()
  } catch {}
  window.dispatchEvent(new Event("session-deleted"))
  // Only create new session if there are no remaining sessions
  if (sessions.value.length === 0) {
    const t = localStorage.getItem('teamix_token')
    if (t) {
      await fetch('/new?token=' + encodeURIComponent(t), { method: 'POST', headers: { 'Content-Type': 'application/json' } })
      sessions.value = await api.sessions()
    }
  }
}
async function resumeSession(s: any, e?: Event) {
  if (s.current) return
  if (e && (e.target as HTMLElement).closest('.session-del')) return
  try {
    await api.resume(s.path)
    // Immediately refresh sessions list to update active state
    api.sessions().then(ss => { sessions.value = Array.isArray(ss) ? ss : [] }).catch(() => {})
    // Clear chat and load new session history
    window.dispatchEvent(new Event("session-deleted"))
    setTimeout(() => {
      window.dispatchEvent(new CustomEvent("session-resumed", { detail: s.path }))
    }, 400)
  } catch (err) {
    console.error("resumeSession error", err)
  }
}
</script>
<template>
  <aside class="sidebar">
    <div class="sidebar__brand" style="position:relative"><svg class="sidebar__logo" viewBox="0 0 24 24" fill="none"><rect width="24" height="24" rx="6" fill="currentColor"/><text x="12" y="16" text-anchor="middle" font-size="14" font-weight="700" fill="#000">T</text></svg><span class="sidebar__name">Teamix</span></div>
    <div class="teamix-user-badge" v-if="userName" style="display:flex;align-items:center;gap:8px;padding:8px 14px 6px;font-size:13px;font-weight:600;color:var(--accent)"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14"><path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/></svg><span>{{ userName }}</span></div>
    <nav class="sidebar__nav">
      <div class="sidebar__item sidebar__item--accent" id="btn-new" @click="newS"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="18" height="18"><path d="M12 20h9"/><path d="M16.5 3.5a2.121 2.121 0 0 1 3 3L7 19l-4 1 1-4L16.5 3.5z"/></svg><span>新会话</span></div>
      <div class="sidebar__item" id="btn-compact" @click="compact" :class="{ 'sidebar__item--disabled': !hasVisibleHistory }"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="18" height="18"><polyline points="4 14 10 14 10 20"/><polyline points="20 10 14 10 14 4"/><line x1="14" y1="10" x2="21" y2="3"/><line x1="3" y1="21" x2="10" y2="14"/></svg><span>压缩</span></div>
      <div class="sidebar__item" id="btn-rewind" @click="rewind" :class="{ 'sidebar__item--disabled': !hasVisibleHistory }"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="18" height="18"><polyline points="1 4 1 10 7 10"/><path d="M3.51 15a9 9 0 1 0 2.13-9.36L1 10"/></svg><span>回退</span></div>
      <div class="sidebar__item" id="btn-tree" @click="emit('branches')"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="18" height="18"><line x1="6" y1="3" x2="6" y2="15"/><circle cx="18" cy="6" r="3"/><circle cx="6" cy="18" r="3"/><path d="M18 9a9 9 0 0 1-9 9"/></svg><span>分支</span></div>
      <div class="sidebar__item" id="btn-summaries" @click="emit('summaries')"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="18" height="18"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="9" y1="13" x2="15" y2="13"/><line x1="9" y1="17" x2="13" y2="17"/></svg><span>总结</span></div>
      <div class="sidebar__item" id="btn-archive" @click="emit('archive')"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="18" height="18"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg><span>会话</span></div>
      <div class="sidebar__item" id="btn-models" @click="emit('models')"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="18" height="18"><rect x="4" y="4" width="16" height="16" rx="2"/><rect x="9" y="9" width="6" height="6"/><path d="M9 1v3"/><path d="M15 1v3"/><path d="M9 20v3"/><path d="M15 20v3"/><path d="M20 9h3"/><path d="M20 14h3"/><path d="M1 9h3"/><path d="M1 14h3"/></svg><span>模型</span></div>
      <div class="sidebar__item" id="btn-workflows" @click="emit('workflows')"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="18" height="18"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg><span>工作流</span></div>
      <div class="sidebar__item" id="btn-settings" @click="emit('settings')"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="18" height="18"><rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/></svg><span>设置</span></div>
      <div class="sidebar__item" id="btn-stats" @click="emit('stats')"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="18" height="18"><path d="M18 20V10"/><path d="M12 20V4"/><path d="M6 20v-6"/></svg><span>统计</span></div>
    </nav>
    <div class="sidebar__resize-h" id="sidebar-resize-h"></div>
    <div class="sidebar__label-row"><span class="sidebar__label">会话</span><span class="session-item__meta">{{ sessions.length }}</span></div>
    <div class="session-search"><input class="session-search__input" id="session-search" type="search" v-model="sessionFilter" placeholder="搜索会话" /></div>
    <div class="session-list" id="session-list" style="flex:1;overflow-y:auto;min-height:0">
      <div v-for="s in filteredSessions" :key="s.path" class="session-item" :class="{ 'session-item--active': s.current }" @click="resumeSession(s, $event)" @contextmenu.prevent="openCtx($event, s)">
        <svg class="session-item__icon" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/></svg>
        <div class="session-item__body"><div class="session-item__title">{{ s.title || s.name }}</div><div class="session-item__meta">{{ s.turns ? s.turns + " 轮" : "" }}</div></div>
        <button type="button" class="session-del" :data-name="s.name" title="归档会话" @click.stop="confirmArchive(s)">&times;</button>
      </div>
      <div v-if="sessions.length === 0" style="padding:10px;color:var(--muted-2);font-size:12px">暂无会话</div>
    </div>
    <div class="sidebar__section">
      <div class="sidebar__label">状态</div>
      <div class="sidebar__ctx"><div class="ctx-bar"><div class="ctx-bar__fill" :style="{ width: ctxFill + '%', background: ctxBarColor }"></div></div><div class="ctx-label"><span>{{ ctxUsed }}</span><span>{{ ctxWindow }}</span></div></div>
      <div class="status-metrics" id="status-metrics">
        <div class="sm-item"><span class="sm-val" id="sm-cache">{{ smCache }}</span><span>缓存</span></div>
        <div class="sm-item"><span class="sm-val" id="sm-cost">{{ smCost }}</span><span>费用</span></div>
        <div class="sm-item"><span class="sm-val acc" id="sm-balance">{{ smBalance }}</span><span>余额</span></div>
      </div>
      <div style="padding:4px 10px"><div class="status"><span class="status__dot" :class="{ 'status__dot--busy': running }"></span><span>{{ statusModel }}</span></div></div>
      <div style="padding:0 10px 6px"><button id="teamix-logout-btn" @click="lgout()" style="width:100%;padding:5px 0;border:1px solid var(--border);border-radius:6px;background:var(--bg-2);color:var(--muted-2);font-size:11px;cursor:pointer">{{ token ? "Logout" : "Login" }}</button></div>
    </div>
  </aside>
  <!-- 会话右键菜单：归档当前 / 归档其他 / 归档全部 -->
  <div v-if="ctxMenu" class="ctx-menu" :style="{ left: ctxMenu.x + 'px', top: ctxMenu.y + 'px' }" @mousedown.stop>
    <div class="ctx-menu__item" @click="confirmArchive(ctxMenu.session)">归档此会话</div>
    <div class="ctx-menu__item" @click="askArchiveOthers">归档其他会话</div>
    <div class="ctx-menu__item ctx-menu__item--danger" @click="askArchiveAll">归档所有会话</div>
  </div>
  <div class="modal-overlay" v-if="showDelete" @click.self="showDelete = false" style="display:flex;z-index:300">
    <div class="modal" style="width:360px">
      <div class="modal__head"><span>归档会话</span><span class="modal__close" @click="showDelete = false">&times;</span></div>
      <div class="modal__body">
        <p v-if="deleteMode === 'one'">确定归档 "{{ deleteName }}"？归档后可在左侧「会话」中查看或恢复。</p>
        <p v-else-if="deleteMode === 'others'">确定归档除当前会话外的所有会话？可在「会话」面板中查看或恢复。</p>
        <p v-else>确定归档所有会话？可在「会话」面板中查看或恢复，归档后将自动新建一个会话。</p>
        <div class="dialog-actions"><button class="dialog-btn" @click="showDelete = false">取消</button><button class="dialog-btn dialog-btn--danger" @click="doArchive">归档</button></div>
      </div>
    </div>
  </div>
</template>
