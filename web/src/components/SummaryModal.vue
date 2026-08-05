<script setup lang="ts">
import { ref, watch } from "vue"
import { api } from "../api"
const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{ (e: "close"): void }>()
const summaries = ref<any[]>([])
const loading = ref(false)
const generating = ref(false)
const err = ref("")
const full = ref<any>(null) // 展开查看的总结条目
const confirmDel = ref<string | null>(null) // 待删除的总结 id（页面风格确认框）
const sessionLabel = ref("") // 当前会话标识（标题或名字）

async function load() {
  if (!props.visible) return
  loading.value = true
  err.value = ""
  try {
    const data = await api.summaries()
    summaries.value = Array.isArray(data) ? data : []
  } catch {
    summaries.value = []
    err.value = "加载失败"
  }
  // 标明这些总结属于哪个会话
  try {
    const ss = await api.sessions()
    const cur = Array.isArray(ss) ? ss.find((x: any) => x.current) : null
    sessionLabel.value = (cur && (cur.title || cur.name)) || ""
  } catch { /* ignore */ }
  loading.value = false
}

watch(() => props.visible, (v) => { if (v) load() })

async function generate() {
  if (generating.value) return
  generating.value = true
  err.value = ""
  try {
    const data = await api.summarizeSession()
    summaries.value = Array.isArray(data) ? data : []
  } catch (e: any) {
    err.value = (e && e.message) || "生成失败"
  }
  generating.value = false
}

async function doConfirmDel() {
  const id = confirmDel.value
  confirmDel.value = null
  if (!id) return
  try {
    const data = await api.deleteSummary(id)
    summaries.value = Array.isArray(data) ? data : []
    if (full.value && full.value.id === id) full.value = null
  } catch (e: any) {
    err.value = (e && e.message) || "删除失败"
  }
}

function summaryTitle(s: any): string {
  if (s && s.title && s.title.trim()) return s.title.trim()
  // 旧数据无标题：取正文前 20 字
  const c = (s && s.content || "").replace(/\s+/g, " ").trim()
  return c ? c.slice(0, 20) + (c.length > 20 ? "…" : "") : "会话总结"
}

function fmtTime(t: string): string {
  if (!t) return ""
  const d = new Date(t)
  if (isNaN(d.getTime())) return ""
  const p = (n: number) => String(n).padStart(2, "0")
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}
</script>

<template>
<div class="modal-overlay" v-if="visible" @click.self="emit('close')" style="z-index:200">
  <div class="modal" style="width:min(520px,92vw);max-height:min(680px,85vh);display:flex;flex-direction:column">
    <div class="modal__head">
      <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="9" y1="13" x2="15" y2="13"/><line x1="9" y1="17" x2="13" y2="17"/></svg>
      <span>会话总结</span>
      <span class="modal__close" @click="emit('close')">&times;</span>
    </div>
    <div style="padding:0 8px 4px;font-size:11px;color:var(--muted-2);overflow:hidden;text-overflow:ellipsis;white-space:nowrap" v-if="sessionLabel">当前会话：{{ sessionLabel }}</div>
    <div style="padding:4px 8px;display:flex;gap:8px;align-items:center">
      <button type="button" class="branch-item__btn" style="margin:0" :disabled="generating" @click="generate">{{ generating ? "生成中…" : "＋ 生成总结" }}</button>
      <span style="font-size:11px;color:var(--muted-2)">为当前会话生成一份人读摘要，不改动会话本身</span>
    </div>
    <div style="padding:0 8px 8px;color:var(--danger);font-size:12px;min-height:0" v-if="err">{{ err }}</div>
    <div class="model-list" style="padding:0 8px 8px;flex:1;min-height:0;overflow-y:auto">
      <div v-if="loading" class="empty-note" style="color:var(--muted-2);text-align:center;padding:16px;font-size:13px">加载中…</div>
      <div v-else-if="summaries.length === 0" class="empty-note" style="color:var(--muted-2);text-align:center;padding:16px;font-size:13px">当前会话还没有总结，点击上方按钮生成一份</div>
      <div v-for="s in summaries" :key="s.id" class="summary-card" @click="full = s">
        <div style="min-width:0;flex:1">
          <div class="summary-card__title">{{ summaryTitle(s) }}</div>
          <div class="branch-item__meta">{{ fmtTime(s.time) }}</div>
          <div class="summary-card__body">{{ s.content }}</div>
        </div>
        <div class="summary-card__actions" @click.stop>
          <button type="button" class="branch-item__btn" title="展开查看" @click="full = s">⛶</button>
          <button type="button" class="branch-item__btn summary-card__del" title="删除" @click="confirmDel = s.id">&times;</button>
        </div>
      </div>
    </div>
  </div>
</div>

<!-- 展开查看（居中大框） -->
<div v-if="full" class="summary-fullscreen" @click.self="full = null">
  <div class="summary-fullscreen__panel">
    <div class="summary-fullscreen__bar">
      <div class="summary-fullscreen__head">
        <div class="summary-fullscreen__title">{{ summaryTitle(full) }}</div>
        <div class="summary-fullscreen__meta">{{ fmtTime(full.time) }}</div>
      </div>
      <div class="summary-fullscreen__ops">
        <button type="button" class="branch-item__btn" @click="full = null">关闭</button>
      </div>
    </div>
    <div class="summary-fullscreen__body">{{ full.content }}</div>
  </div>
</div>

<!-- 页面风格删除确认框 -->
<div class="modal-overlay" v-if="confirmDel" @click.self="confirmDel = null" style="display:flex;z-index:320">
  <div class="modal" style="width:340px">
    <div class="modal__head"><span>删除总结</span><span class="modal__close" @click="confirmDel = null">&times;</span></div>
    <div class="modal__body"><p>确定删除这条会话总结？</p><div class="dialog-actions"><button class="dialog-btn" @click="confirmDel = null">取消</button><button class="dialog-btn dialog-btn--danger" @click="doConfirmDel">删除</button></div></div>
  </div>
</div>
</template>

<style scoped>
.summary-card {
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
.summary-card:hover { border-color: var(--accent); background: var(--card-hover); }
.summary-card__title { font-size: 13px; font-weight: 600; color: var(--fg); }
.summary-card__actions { display: flex; gap: 4px; flex-shrink: 0; }
.summary-card__del { color: var(--danger); }
/* 内容默认折叠，悬浮时直接在卡片内展开（不弹浮层） */
.summary-card__body {
  max-height: 0;
  overflow: hidden;
  transition: max-height .25s ease;
  font-size: 12px;
  line-height: 1.65;
  color: var(--muted);
  white-space: pre-wrap;
  word-break: break-word;
}
.summary-card:hover .summary-card__body {
  max-height: 360px;
  overflow-y: auto;
}

.summary-fullscreen {
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
.summary-fullscreen__panel {
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
.summary-fullscreen__bar {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  padding-bottom: 14px;
  border-bottom: 1px solid var(--border);
  margin-bottom: 14px;
  flex-shrink: 0;
}
.summary-fullscreen__head { min-width: 0; }
.summary-fullscreen__title { font-size: 17px; font-weight: 700; color: var(--fg); }
.summary-fullscreen__meta { font-size: 12px; color: var(--muted-2); margin-top: 4px; }
.summary-fullscreen__ops { display: flex; gap: 8px; flex-shrink: 0; }
.summary-fullscreen__body {
  flex: 1;
  overflow-y: auto;
  font-size: 14px;
  line-height: 1.8;
  color: var(--fg-2);
  white-space: pre-wrap;
  word-break: break-word;
}
</style>
