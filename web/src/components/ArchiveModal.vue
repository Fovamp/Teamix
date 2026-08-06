<script setup lang="ts">
import { ref, watch } from "vue"
import { api } from "../api"

// 会话归档面板（假删除）：红叉/右键删除的会话在这里，可查看每轮内容、恢复；
// 此处删除才是永久删除。样式仿「会话总结」面板。
const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{ (e: "close"): void }>()

const archives = ref<any[]>([])
const loading = ref(false)
const err = ref("")
const full = ref<any | null>(null)   // 展开查看的归档项
const turns = ref<any[][]>([])       // 展开的轮次内容
const loadingTurns = ref(false)
const confirmDel = ref<string | null>(null)
const confirmRestore = ref<string | null>(null)

function fmtTime(t: string): string {
  if (!t) return ""
  const d = new Date(t)
  if (isNaN(d.getTime())) return ""
  const p = (n: number) => String(n).padStart(2, "0")
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

function titleOf(a: any): string {
  const t = (a && a.title || "").trim()
  return t || (a && a.name) || "归档会话"
}

async function load() {
  loading.value = true
  err.value = ""
  try {
    const data = await api.archiveList()
    archives.value = (data && data.archives) || []
  } catch (e: any) {
    err.value = (e && e.message) || "加载失败"
  }
  loading.value = false
}

watch(() => props.visible, (v) => { if (v) load() })

async function openFull(a: any) {
  if (full.value && full.value.name === a.name) { full.value = null; turns.value = []; return }
  full.value = a
  turns.value = []
  loadingTurns.value = true
  try {
    const data = await api.archiveRead(a.name)
    turns.value = (data && data.turns) || []
  } catch (e: any) {
    err.value = (e && e.message) || "读取失败"
  }
  loadingTurns.value = false
}

async function doRestore() {
  if (!confirmRestore.value) return
  try {
    await api.archiveRestore(confirmRestore.value)
    confirmRestore.value = null
    // 立即刷新左侧会话栏（不依赖整页刷新）
    try {
      const ss = await api.sessions()
      window.dispatchEvent(new CustomEvent('sessions-update', { detail: Array.isArray(ss) ? ss : [] }))
    } catch {}
    await load()
  } catch (e: any) {
    err.value = (e && e.message) || "恢复失败"
    confirmRestore.value = null
  }
}

async function doDelete() {
  if (!confirmDel.value) return
  try {
    await api.archiveDelete(confirmDel.value)
    if (full.value && full.value.name === confirmDel.value) { full.value = null; turns.value = [] }
  } catch (e: any) {
    err.value = (e && e.message) || "删除失败"
  }
  confirmDel.value = null
  await load()
}
</script>

<template>
<div class="modal-overlay" v-if="visible" @click.self="emit('close')" style="z-index:200">
  <div class="modal" style="width:min(540px,92vw);max-height:min(680px,85vh);display:flex;flex-direction:column">
    <div class="modal__head">
      <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg>
      <span>会话归档</span>
      <span class="modal__close" @click="emit('close')">&times;</span>
    </div>
    <div style="padding:4px 8px;font-size:11px;color:var(--muted-2)">红叉删除的会话归档在这里，可查看每轮内容或恢复；此处「删除」为永久删除。</div>
    <div style="padding:0 8px 8px;color:var(--danger);font-size:12px;min-height:0" v-if="err">{{ err }}</div>
    <div class="model-list" style="padding:0 8px 8px;flex:1;min-height:0;overflow-y:auto">
      <div v-if="loading" class="empty-note" style="color:var(--muted-2);text-align:center;padding:16px;font-size:13px">加载中…</div>
      <div v-else-if="archives.length === 0" class="empty-note" style="color:var(--muted-2);text-align:center;padding:16px;font-size:13px">暂无归档会话</div>
      <div v-for="a in archives" :key="a.name" class="arch-card" @click="openFull(a)">
        <div style="min-width:0;flex:1">
          <div class="arch-card__title" :title="titleOf(a)">{{ titleOf(a) }}</div>
          <div class="arch-card__meta">{{ fmtTime(a.time) }} · {{ a.turns }} 轮 · {{ a.project }}</div>
          <div class="arch-card__body">{{ a.title ? '' : (a.name || '') }}</div>
        </div>
        <div class="arch-card__actions" @click.stop>
          <button type="button" class="branch-item__btn" title="展开查看" @click="openFull(a)">⛶</button>
          <button type="button" class="branch-item__btn" title="恢复会话" @click="confirmRestore = a.name">恢复</button>
          <button type="button" class="branch-item__btn arch-card__del" title="永久删除" @click="confirmDel = a.name">&times;</button>
        </div>
      </div>
    </div>
  </div>
</div>

<!-- 展开查看（居中大框，仿总结面板） -->
<div v-if="full" class="arch-fullscreen" @click.self="full = null">
  <div class="arch-fullscreen__panel">
    <div class="arch-fullscreen__bar">
      <div class="arch-fullscreen__head">
        <div class="arch-fullscreen__title">{{ titleOf(full) }}</div>
        <div class="arch-fullscreen__meta">{{ fmtTime(full.time) }} · {{ full.turns }} 轮 · {{ full.project }}</div>
      </div>
      <div class="arch-fullscreen__ops">
        <button type="button" class="branch-item__btn" @click="full = null">关闭</button>
      </div>
    </div>
    <div class="arch-fullscreen__body">
      <div v-if="loadingTurns" style="color:var(--muted-2)">加载中…</div>
      <div v-else-if="turns.length === 0" style="color:var(--muted-2)">（无用户轮次内容）</div>
      <div v-for="(turn, ti) in turns" :key="ti" class="arch-turn">
        <div class="arch-turn__label">第 {{ ti + 1 }} 轮</div>
        <div v-for="(m, mi) in turn" :key="mi" class="arch-msg" :class="m.role === 'user' ? 'arch-msg--user' : m.role === 'assistant' ? 'arch-msg--ai' : ''">
          <span class="arch-msg__role">{{ m.role === 'user' ? '用户' : m.role === 'assistant' ? 'AI' : m.role }}</span>
          <span class="arch-msg__content">{{ m.content }}</span>
        </div>
      </div>
    </div>
  </div>
</div>

<!-- 恢复确认框 -->
<div v-if="confirmRestore" class="modal-overlay" @click.self="confirmRestore = null" style="display:flex;z-index:320">
  <div class="modal" style="width:340px">
    <div class="modal__head"><span>恢复会话</span><span class="modal__close" @click="confirmRestore = null">&times;</span></div>
    <div class="modal__body"><p>确定恢复会话 "{{ confirmRestore }}"？恢复后回到左侧会话列表。</p><div class="dialog-actions"><button class="dialog-btn" @click="confirmRestore = null">取消</button><button class="dialog-btn dialog-btn--danger" @click="doRestore">恢复</button></div></div>
  </div>
</div>

<!-- 永久删除确认框 -->
<div v-if="confirmDel" class="modal-overlay" @click.self="confirmDel = null" style="display:flex;z-index:320">
  <div class="modal" style="width:340px">
    <div class="modal__head"><span>永久删除</span><span class="modal__close" @click="confirmDel = null">&times;</span></div>
    <div class="modal__body"><p>确定永久删除会话 "{{ confirmDel }}"？<b>此操作不可恢复</b>，文件将被物理删除。</p><div class="dialog-actions"><button class="dialog-btn" @click="confirmDel = null">取消</button><button class="dialog-btn dialog-btn--danger" @click="doDelete">永久删除</button></div></div>
  </div>
</div>
</template>

<style scoped>
/* 列表卡片（仿总结面板 .summary-card） */
.arch-card {
  position: relative;
  display: flex;
  gap: 8px;
  align-items: flex-start;
  padding: 9px 10px;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--bg-2);
  cursor: pointer;
  transition: all .15s;
  margin-bottom: 6px;
}
.arch-card:hover { border-color: var(--accent); background: var(--card-hover); }
.arch-card__title { font-size: 13px; font-weight: 600; color: var(--fg); }
.arch-card__meta { font-size: 11px; color: var(--muted-2); margin-top: 2px; }
.arch-card__body { font-size: 12px; line-height: 1.65; color: var(--muted); white-space: pre-wrap; word-break: break-word; }
.arch-card__actions { display: flex; gap: 4px; flex-shrink: 0; align-items: center; }
.arch-card__del { color: var(--danger); }

/* 展开大框（仿总结面板 .summary-fullscreen） */
.arch-fullscreen {
  position: fixed;
  inset: 0;
  z-index: 500;
  background: oklch(0% 0 0 / 0.55);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  box-sizing: border-box;
}
.arch-fullscreen__panel {
  width: min(760px, 92vw);
  height: min(76vh, 720px);
  background: var(--panel, #14181f);
  border: 1px solid var(--border-strong, rgba(255,255,255,.14));
  border-radius: var(--radius-lg, 14px);
  box-shadow: 0 14px 44px rgba(0,0,0,.55);
  display: flex;
  flex-direction: column;
  padding: 18px 22px;
  box-sizing: border-box;
}
.arch-fullscreen__bar {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  padding-bottom: 14px;
  border-bottom: 1px solid var(--border);
  margin-bottom: 14px;
  flex-shrink: 0;
}
.arch-fullscreen__head { min-width: 0; }
.arch-fullscreen__title { font-size: 17px; font-weight: 700; color: var(--fg); }
.arch-fullscreen__meta { font-size: 12px; color: var(--muted-2); margin-top: 4px; }
.arch-fullscreen__ops { display: flex; gap: 8px; flex-shrink: 0; }
.arch-fullscreen__body {
  flex: 1;
  overflow-y: auto;
  font-size: 14px;
  line-height: 1.8;
  color: var(--fg-2);
}
.arch-turn { margin-bottom: 14px; padding-bottom: 12px; border-bottom: 1px dashed var(--border); }
.arch-turn:last-child { border-bottom: none; }
.arch-turn__label { font-size: 11px; color: var(--muted-2); margin-bottom: 6px; font-weight: 600; }
.arch-msg { margin-bottom: 6px; }
.arch-msg__role { font-size: 11px; font-weight: 600; color: var(--muted-2); margin-right: 8px; }
.arch-msg--user .arch-msg__role { color: var(--accent); }
.arch-msg--ai .arch-msg__role { color: var(--muted-2); }
.arch-msg__content { white-space: pre-wrap; word-break: break-word; font-size: 13px; }
</style>
