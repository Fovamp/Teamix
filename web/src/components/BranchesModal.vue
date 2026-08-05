<script setup lang="ts">
import { ref, watch } from "vue"
import { api } from "../api"
const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{ (e: "close"): void }>()
const branches = ref<any[]>([])
const treeText = ref("")
const loading = ref(false)

watch(() => props.visible, async (v) => {
  if (v) {
    loading.value = true
    try {
      const data = await api.branches()
      treeText.value = data?.tree || "No branches"
      branches.value = Array.isArray(data?.branches) ? data.branches : []
    } catch {
      treeText.value = "加载失败"
    }
    loading.value = false
  }
})

function branchValue(b: any, ...keys: string[]) {
  for (const k of keys) { if (b && b[k] != null && b[k] !== '') return b[k] }
  return ''
}
function branchTitle(b: any) {
  return branchValue(b, 'custom_title', 'CustomTitle', 'name', 'Name', 'topic_title', 'TopicTitle', 'id', 'ID') || 'branch'
}
// 分叉点标注：分支有 fork_turn/parent_id 时显示"分叉自 <父分支> · 第 N 轮"；主线分支返回空。
// fork_turn=-1 表示 tip 分叉（从最新消息分叉），无轮数标注。
// 优先显示后端补的父会话标题（parent_title），无标题时才回退到父 ID 前 8 位。
function forkLabel(b: any): string {
  const ft = branchValue(b, 'fork_turn', 'ForkTurn')
  if (ft === '' && ft !== 0) return ''
  const pid = branchValue(b, 'parent_id', 'ParentID')
  const parentTitle = branchValue(b, 'parent_title', 'ParentTitle')
  const parent = parentTitle || (pid ? String(pid).slice(0, 8) : '未知')
  if (ft < 0) return `分叉自 ${parent}`
  return `分叉自 ${parent} · 第 ${ft} 轮`
}
function isFork(b: any): boolean {
  const ft = branchValue(b, 'fork_turn', 'ForkTurn')
  return ft !== '' && ft !== 0
}
async function switchBranch(id: string) {
  if (!id) return
  emit("close")
  const t = localStorage.getItem("teamix_token")
  if (!t) return
  try {
    const r = await fetch("/teamix/branch/switch?token=" + encodeURIComponent(t), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ ref: id }),
    })
    // 切换成功后：ChatArea 监听该事件重新加载该分支历史 + 刷新会话列表
    if (r.ok) window.dispatchEvent(new CustomEvent("teamix-session-changed"))
  } catch { /* ignore */ }
}
</script>

<template>
<div class="modal-overlay" v-if="visible" @click.self="emit('close')" style="z-index:200">
  <div class="modal" style="width:min(480px,90vw);max-height:min(680px,85vh);display:flex;flex-direction:column">
    <div class="modal__head">
      <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="6" y1="3" x2="6" y2="15"/><circle cx="18" cy="6" r="3"/><circle cx="6" cy="18" r="3"/><path d="M18 9a9 9 0 0 1-9 9"/></svg>
      <span>分支</span>
      <span class="modal__close" @click="emit('close')">&times;</span>
    </div>
    <div style="flex:1;min-height:0;overflow-y:auto;display:flex;flex-direction:column">
      <div style="font-family:var(--mono);font-size:11px;color:var(--muted);padding:4px 8px;word-break:break-all;overflow-wrap:break-word;white-space:pre-wrap" id="branches-tree">{{ treeText }}</div>
      <div class="model-list" id="branches-list" style="padding:0 8px 8px">
      <div v-if="branches.length === 0 && !loading" class="empty-note" style="color:var(--muted-2);text-align:center;padding:16px;font-size:13px">暂无会话分支</div>
      <div v-for="b in branches" :key="branchValue(b, 'id', 'ID')" class="branch-item" :title="branchTitle(b) + ' — ' + [branchValue(b, 'turns', 'Turns') ? branchValue(b, 'turns', 'Turns') + ' turns' : '', branchValue(b, 'model', 'Model'), branchValue(b, 'preview', 'Preview')].filter(Boolean).join(' · ')">
        <div style="min-width:0;overflow:hidden">
          <div class="branch-item__title">{{ isFork(b) ? '🔀 ' : '' }}{{ branchTitle(b) }}</div>
          <div class="branch-item__meta">{{ forkLabel(b) || [branchValue(b, 'turns', 'Turns') ? branchValue(b, 'turns', 'Turns') + ' turns' : '', branchValue(b, 'model', 'Model'), branchValue(b, 'preview', 'Preview')].filter(Boolean).join(' · ') || branchValue(b, 'id', 'ID') }}</div>
        </div>
        <button class="branch-item__btn" @click="switchBranch(branchValue(b, 'id', 'ID'))">切换</button>
      </div>
    </div>
    </div>
  </div>
</div>
</template>

<style scoped>
.branch-item { display: grid; grid-template-columns: 1fr auto; gap: 8px; align-items: center; padding: 9px 10px; border: 1px solid var(--border); border-radius: var(--radius); background: var(--bg-2); cursor: pointer; transition: all .15s; }
.branch-item:hover { border-color: var(--accent); background: var(--card-hover); }
.branch-item__title { font-family: var(--mono); font-size: 12.5px; color: var(--fg); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.branch-item__meta { font-size: 11px; color: var(--muted); margin-top: 2px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.branch-item__btn { padding: 4px 10px; border: 1px solid var(--border); border-radius: 4px; background: var(--bg-2); color: var(--fg-2); font-size: 11px; cursor: pointer; transition: all .15s; }
.branch-item__btn:hover { border-color: var(--border-strong); color: var(--fg); }
</style>
