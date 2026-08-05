<script setup lang="ts">
import { ref, watch } from "vue"
import { api } from "../api"
const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{ (e: "close"): void }>()
const summaries = ref<any[]>([])
const loading = ref(false)
const generating = ref(false)
const err = ref("")

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
    <div style="padding:8px;display:flex;gap:8px;align-items:center">
      <button type="button" class="branch-item__btn" style="margin:0" :disabled="generating" @click="generate">{{ generating ? "生成中…" : "＋ 生成总结" }}</button>
      <span style="font-size:11px;color:var(--muted-2)">为当前会话生成一份人读摘要，不改动会话本身</span>
    </div>
    <div style="padding:0 8px 8px;color:var(--danger);font-size:12px;min-height:0" v-if="err">{{ err }}</div>
    <div class="model-list" style="padding:0 8px 8px;flex:1;min-height:0;overflow-y:auto">
      <div v-if="loading" class="empty-note" style="color:var(--muted-2);text-align:center;padding:16px;font-size:13px">加载中…</div>
      <div v-else-if="summaries.length === 0" class="empty-note" style="color:var(--muted-2);text-align:center;padding:16px;font-size:13px">当前会话还没有总结，点击上方按钮生成一份</div>
      <div v-for="s in summaries" :key="s.id" class="branch-item" style="align-items:flex-start">
        <div style="min-width:0;flex:1">
          <div class="branch-item__meta">{{ fmtTime(s.time) }}</div>
          <div class="summary-item__content">{{ s.content }}</div>
        </div>
      </div>
    </div>
  </div>
</div>
</template>

<style scoped>
.summary-item__content {
  font-size: 12px;
  line-height: 1.6;
  color: var(--fg-2, #cbd5e1);
  white-space: pre-wrap;
  word-break: break-word;
  margin-top: 4px;
  padding: 6px 8px;
  background: var(--panel-2, rgba(255,255,255,0.04));
  border-radius: 6px;
  border: 1px solid var(--border, rgba(255,255,255,0.08));
}
</style>
