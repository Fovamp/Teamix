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
async function switchBranch(id: string) {
  if (!id) return
  emit("close")
  try { await api.submit('/switch ' + id) } catch {}
}
</script>

<template>
<div class="modal-overlay" v-if="visible" @click.self="emit('close')" style="z-index:200">
  <div class="modal" style="width:min(480px,90vw)">
    <div class="modal__head">
      <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="6" y1="3" x2="6" y2="15"/><circle cx="18" cy="6" r="3"/><circle cx="6" cy="18" r="3"/><path d="M18 9a9 9 0 0 1-9 9"/></svg>
      <span>分支</span>
      <span class="modal__close" @click="emit('close')">&times;</span>
    </div>
    <div style="padding:8px">
      <div style="font-family:var(--mono);font-size:11px;color:var(--muted);padding:4px 8px" id="branches-tree">{{ treeText }}</div>
    </div>
    <div class="model-list" id="branches-list" style="padding:0 8px 8px;max-height:40vh;overflow-y:auto">
      <div v-if="branches.length === 0 && !loading" class="empty-note" style="color:var(--muted-2);text-align:center;padding:16px;font-size:13px">暂无会话分支</div>
      <div v-for="b in branches" :key="branchValue(b, 'id', 'ID')" class="branch-item">
        <div>
          <div class="branch-item__title">{{ branchTitle(b) }}</div>
          <div class="branch-item__meta">{{ [branchValue(b, 'turns', 'Turns') ? branchValue(b, 'turns', 'Turns') + ' turns' : '', branchValue(b, 'model', 'Model'), branchValue(b, 'preview', 'Preview')].filter(Boolean).join(' · ') || branchValue(b, 'id', 'ID') }}</div>
        </div>
        <button class="branch-item__btn" @click="switchBranch(branchValue(b, 'id', 'ID'))">切换</button>
      </div>
    </div>
  </div>
</div>
</template>
