<script setup lang="ts">
import { ref, watch } from "vue"
import { api } from "../api"

// 会话归档面板：红叉/右键删除的会话（假删除）在这里，可查看每轮内容、恢复；
// 此处删除才是永久删除。
const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{ (e: "close"): void; (e: "restored"): void }>()

const archives = ref<any[]>([])
const loading = ref(false)
const err = ref("")
const full = ref("")           // 展开查看的会话 name
const turns = ref<any[][]>([]) // 展开的轮次内容
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

async function openFull(name: string) {
  if (full.value === name) { full.value = ""; turns.value = []; return }
  full.value = name
  turns.value = []
  loadingTurns.value = true
  try {
    const data = await api.archiveRead(name)
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
    emit("restored")
  } catch (e: any) {
    err.value = (e && e.message) || "恢复失败"
  }
  confirmRestore.value = null
  await load()
}

async function doDelete() {
  if (!confirmDel.value) return
  try {
    await api.archiveDelete(confirmDel.value)
    if (full.value === confirmDel.value) { full.value = ""; turns.value = [] }
  } catch (e: any) {
    err.value = (e && e.message) || "删除失败"
  }
  confirmDel.value = null
  await load()
}

function turnPreview(turn: any[]): string {
  const first = (turn || []).find((m: any) => m.role === "user")
  return (first && first.content || "").replace(/\s+/g, " ").slice(0, 60)
}
</script>

<template>
<div class="modal-overlay" v-if="visible" @click.self="emit('close')" style="z-index:200">
  <div class="modal" style="width:min(600px,92vw);max-height:min(720px,85vh);display:flex;flex-direction:column">
    <div class="modal__head">
      <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg>
      <span>会话归档</span>
      <span class="modal__close" @click="emit('close')">&times;</span>
    </div>
    <div style="padding:4px 8px;font-size:11px;color:var(--muted-2)">红叉删除的会话归档在这里（假删除），可查看每轮内容或恢复；此处「删除」为永久删除，不可恢复。</div>
    <div style="padding:0 8px 8px;color:var(--danger);font-size:12px;min-height:0" v-if="err">{{ err }}</div>
    <div class="model-list" style="padding:0 8px 8px;flex:1;min-height:0;overflow-y:auto">
      <div v-if="loading" class="empty-note" style="color:var(--muted-2);text-align:center;padding:16px;font-size:13px">加载中…</div>
      <div v-else-if="archives.length === 0" class="empty-note" style="color:var(--muted-2);text-align:center;padding:16px;font-size:13px">暂无归档会话</div>
      <div v-for="a in archives" :key="a.name" class="summary-card" @click="openFull(a.name)">
        <div style="min-width:0;flex:1">
          <div class="summary-card__title" :title="a.title || a.name">{{ a.title || a.name }}</div>
          <div class="branch-item__meta">{{ fmtTime(a.time) }} · {{ a.turns }} 轮 · {{ a.project }}</div>
          <div v-if="full === a.name" class="summary-card__body" style="margin-top:6px;border-top:1px solid var(--border);padding-top:6px">
            <div v-if="loadingTurns" style="color:var(--muted-2)">加载中…</div>
            <div v-else-if="turns.length === 0" style="color:var(--muted-2)">（无用户轮次内容）</div>
            <div v-for="(turn, ti) in turns" :key="ti" style="margin-bottom:8px">
              <div style="font-size:11px;color:var(--muted-2);margin-bottom:2px">第 {{ ti + 1 }} 轮</div>
              <div v-for="(m, mi) in turn" :key="mi" style="margin-bottom:4px">
                <span :style="{ fontSize: '11px', fontWeight: 600, color: m.role === 'user' ? 'var(--accent)' : 'var(--muted-2)', marginRight: '6px' }">{{ m.role === 'user' ? '用户' : m.role === 'assistant' ? 'AI' : m.role }}</span>
                <span style="white-space:pre-wrap;word-break:break-word;font-size:12px;color:var(--fg)">{{ m.content }}</span>
              </div>
            </div>
          </div>
        </div>
        <div class="summary-card__actions" @click.stop>
          <button type="button" class="branch-item__btn" title="查看内容" @click="openFull(a.name)">{{ full === a.name ? '收起' : '展开' }}</button>
          <button type="button" class="branch-item__btn" title="恢复会话" @click="confirmRestore = a.name">恢复</button>
          <button type="button" class="branch-item__btn summary-card__del" title="永久删除" @click="confirmDel = a.name">&times;</button>
        </div>
      </div>
    </div>
  </div>
</div>

<!-- 恢复确认框 -->
<div v-if="confirmRestore" class="modal-overlay" @click.self="confirmRestore = null" style="z-index:220">
  <div class="modal" style="width:360px">
    <div class="modal__head"><span>恢复会话</span><span class="modal__close" @click="confirmRestore = null">&times;</span></div>
    <div class="modal__body"><p>确定恢复会话 "{{ confirmRestore }}"？恢复后回到左侧会话列表。</p>
      <div class="dialog-actions"><button class="dialog-btn" @click="confirmRestore = null">取消</button><button class="dialog-btn dialog-btn--danger" @click="doRestore">恢复</button></div>
    </div>
  </div>
</div>

<!-- 永久删除确认框 -->
<div v-if="confirmDel" class="modal-overlay" @click.self="confirmDel = null" style="z-index:220">
  <div class="modal" style="width:360px">
    <div class="modal__head"><span>永久删除</span><span class="modal__close" @click="confirmDel = null">&times;</span></div>
    <div class="modal__body"><p>确定永久删除会话 "{{ confirmDel }}"？<b>此操作不可恢复</b>，文件将被物理删除。</p>
      <div class="dialog-actions"><button class="dialog-btn" @click="confirmDel = null">取消</button><button class="dialog-btn dialog-btn--danger" @click="doDelete">永久删除</button></div>
    </div>
  </div>
</div>
</template>
