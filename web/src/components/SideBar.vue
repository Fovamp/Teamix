<script setup lang="ts">
import { ref, computed, onMounted } from "vue"
import { api } from "../api"
const emit = defineEmits<{ (e: "stats"): void; (e: "branches"): void; (e: "models"): void; (e: "workflows"): void; (e: "settings"): void }>()
const sessions = ref<any[]>([])
const userName = ref("")
const token = ref(localStorage.getItem("teamix_token") || "")
const running = ref(false)
const ctxFill = ref(0)
const ctxUsed = ref("0 tok")
const ctxWindow = ref("0 tok")
const statusModel = ref("-")
const smCache = ref("—")
const smCost = ref("—")
const smBalance = ref("—")
const deleteName = ref("")
const showDelete = ref(false)
const sessionFilter = ref("")
const filteredSessions = computed(() => {
  const q = sessionFilter.value.trim().toLowerCase()
  if (!q) return sessions.value
  return sessions.value.filter((s: any) => (s.title || s.name || "").toLowerCase().includes(q))
})
onMounted(async () => {
  try { const r = await api.userRole(); userName.value = r.user } catch {}
  try { const s = await api.status(); if (s && s.window > 0) { ctxFill.value = Math.round((s.used / s.window) * 100); const f = (n: number) => (n >= 1000 ? (n / 1000).toFixed(1).replace(/\.?0$/, "") + "k" : String(n)) + " tok"; ctxUsed.value = f(s.used); ctxWindow.value = f(s.window) }; statusModel.value = s?.label || "-"; running.value = s?.running || false
  try { const st = await api.status(); if (st) { var _t = (st.cacheHit || 0) + (st.cacheMiss || 0); smCache.value = _t > 0 ? Math.round((st.cacheHit || 0) / _t * 100) + "%" : "-"; smCost.value = st.totalCost ? "$" + st.totalCost.toFixed(4) : "-"; if (st.balance && st.balance.display) { smBalance.value = st.balance.display } else { smBalance.value = "—" } } } catch {} } catch {}
  try { sessions.value = await api.sessions() } catch {}
})
async function newS() { try { await api.newSession(); sessions.value = await api.sessions() } catch {} }
async function compact() { try { await api.compact() } catch {} }
async function rewind() { try { const c = await api.checkpoints(); if (c?.length) await api.rewind(c[c.length-1].turn) } catch {} }
function lgout() { api.logout(); location.reload() }
function confirmDelete(name: string) { deleteName.value = name; showDelete.value = true }
async function doDelete() {
  try { await api.deleteSession(deleteName.value); showDelete.value = false; sessions.value = await api.sessions() } catch {}
}
</script>
<template>
  <aside class="sidebar">
    <div class="sidebar__brand"><svg class="sidebar__logo" viewBox="0 0 24 24" fill="none"><rect width="24" height="24" rx="6" fill="currentColor"/><text x="12" y="16" text-anchor="middle" font-size="14" font-weight="700" fill="#000">T</text></svg><span class="sidebar__name">Teamix</span></div>
    <div class="teamix-user-badge" v-if="userName" style="display:flex;align-items:center;gap:8px;padding:8px 14px 6px;font-size:13px;font-weight:600;color:var(--accent)"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14"><path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/></svg><span>{{ userName }}</span></div>
    <nav class="sidebar__nav">
      <div class="sidebar__item sidebar__item--accent" id="btn-new" @click="newS"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="18" height="18"><path d="M12 20h9"/><path d="M16.5 3.5a2.121 2.121 0 0 1 3 3L7 19l-4 1 1-4L16.5 3.5z"/></svg><span>新会话</span></div>
      <div class="sidebar__item" id="btn-compact" @click="compact"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="18" height="18"><polyline points="4 14 10 14 10 20"/><polyline points="20 10 14 10 14 4"/><line x1="14" y1="10" x2="21" y2="3"/><line x1="3" y1="21" x2="10" y2="14"/></svg><span>压缩</span></div>
      <div class="sidebar__item" id="btn-rewind" @click="rewind"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="18" height="18"><polyline points="1 4 1 10 7 10"/><path d="M3.51 15a9 9 0 1 0 2.13-9.36L1 10"/></svg><span>回退</span></div>
      <div class="sidebar__item" id="btn-tree" @click="emit('branches')"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="18" height="18"><line x1="6" y1="3" x2="6" y2="15"/><circle cx="18" cy="6" r="3"/><circle cx="6" cy="18" r="3"/><path d="M18 9a9 9 0 0 1-9 9"/></svg><span>分支</span></div>
      <div class="sidebar__item" id="btn-models" @click="emit('models')"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="18" height="18"><rect x="4" y="4" width="16" height="16" rx="2"/><rect x="9" y="9" width="6" height="6"/><path d="M9 1v3"/><path d="M15 1v3"/><path d="M9 20v3"/><path d="M15 20v3"/><path d="M20 9h3"/><path d="M20 14h3"/><path d="M1 9h3"/><path d="M1 14h3"/></svg><span>模型</span></div>
      <div class="sidebar__item" id="btn-workflows" @click="emit('workflows')"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="18" height="18"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg><span>工作流</span></div>
      <div class="sidebar__item" id="btn-settings" @click="emit('settings')"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="18" height="18"><rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/></svg><span>设置</span></div>
      <div class="sidebar__item" id="btn-stats" @click="emit('stats')"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="18" height="18"><path d="M18 20V10"/><path d="M12 20V4"/><path d="M6 20v-6"/></svg><span>统计</span></div>
    </nav>
    <div class="sidebar__resize-h" id="sidebar-resize-h"></div>
    <div class="sidebar__label-row"><span class="sidebar__label">会话</span><span class="session-item__meta">{{ sessions.length }}</span></div>
    <div class="session-search"><input class="session-search__input" id="session-search" type="search" v-model="sessionFilter" placeholder="搜索会话" /></div>
    <div class="session-list" id="session-list" style="flex:1;overflow-y:auto;min-height:0">
      <div v-for="s in filteredSessions" :key="s.path" class="session-item" :class="{ 'session-item--active': s.current }">
        <svg class="session-item__icon" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/></svg>
        <div class="session-item__body" @click="api.resume(s.path)"><div class="session-item__title">{{ s.title || s.name }}</div><div class="session-item__meta">{{ s.turns ? s.turns + " 轮" : "" }}</div></div>
        <button type="button" class="session-del" :data-name="s.name" title="删除会话" @click.stop="confirmDelete(s.name)">&times;</button>
      </div>
      <div v-if="sessions.length === 0" style="padding:10px;color:var(--muted-2);font-size:12px">暂无会话</div>
    </div>
    <div class="sidebar__section">
      <div class="sidebar__label">状态</div>
      <div class="sidebar__ctx"><div class="ctx-bar"><div class="ctx-bar__fill" :style="{ width: ctxFill + '%' }"></div></div><div class="ctx-label"><span>{{ ctxUsed }}</span><span>{{ ctxWindow }}</span></div></div>
      <div class="status-metrics" id="status-metrics">
        <div class="sm-item"><span class="sm-val" id="sm-cache">{{ smCache }}</span><span>缓存</span></div>
        <div class="sm-item"><span class="sm-val" id="sm-cost">{{ smCost }}</span><span>费用</span></div>
        <div class="sm-item"><span class="sm-val acc" id="sm-balance">{{ smBalance }}</span><span>余额</span></div>
      </div>
      <div style="padding:4px 10px"><div class="status"><span class="status__dot" :class="{ 'status__dot--busy': running }"></span><span>{{ running ? "思考中..." : statusModel }}</span></div></div>
      <div style="padding:0 10px 6px"><button id="teamix-logout-btn" @click="lgout()" style="width:100%;padding:5px 0;border:1px solid var(--border);border-radius:6px;background:var(--bg-2);color:var(--muted-2);font-size:11px;cursor:pointer">{{ token ? "Logout" : "Login" }}</button></div>
    </div>
  </aside>
  <div class="modal-overlay" v-if="showDelete" @click.self="showDelete = false" style="display:flex;z-index:300">
    <div class="modal" style="width:360px">
      <div class="modal__head"><span>删除会话</span><span class="modal__close" @click="showDelete = false">&times;</span></div>
      <div class="modal__body"><p>确定删除 "{{ deleteName }}"？</p><div class="dialog-actions"><button class="dialog-btn" @click="showDelete = false">取消</button><button class="dialog-btn dialog-btn--danger" @click="doDelete">删除</button></div></div>
    </div>
  </div>
</template>
