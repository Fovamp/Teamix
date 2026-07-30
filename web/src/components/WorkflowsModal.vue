<script setup lang="ts">
import { ref, watch, onMounted } from "vue"
import { api } from "../api"
const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{ (e: "close"): void }>()
const templates = ref<any[]>([])
const loading = ref(false)
const isArchitect = ref(false)

onMounted(async () => {
  try { const r = await api.userRole(); isArchitect.value = r.role === 'architect' } catch {}
})

watch(() => props.visible, async (v) => {
  if (v) {
    loading.value = true
    try {
      const ts = await api.workflowTemplates()
      templates.value = Array.isArray(ts) ? ts : []
    } catch { templates.value = [] }
    loading.value = false
  }
})

async function selectWf(name: string) {
  if (!name) return
  try { await api.workflowSelect(name) } catch {}
  emit("close")
  if (name === 'none') {
    // Clear workflow from localStorage
    localStorage.removeItem('teamix_wf_name')
    window.dispatchEvent(new CustomEvent("workflow-changed"))
    window.dispatchEvent(new CustomEvent("workflow-selected", { detail: '' }))
    return
  }
  const tpl = templates.value.find((t: any) => t.name === name)
  const label = tpl?.label || tpl?.name || name
  window.dispatchEvent(new CustomEvent("workflow-changed"))
  window.dispatchEvent(new CustomEvent("workflow-selected", { detail: label }))
}

async function deleteWf(name: string) {
  if (!name || name === 'none') return
  try {
    await fetch("/teamix/workflows/template/delete?token=" + encodeURIComponent(localStorage.getItem("teamix_token") || ""), {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name })
    })
    // Refresh
    const ts = await api.workflowTemplates()
    templates.value = Array.isArray(ts) ? ts : []
  } catch {}
}
</script>

<template>
  <div class="modal-overlay" v-if="visible" @click.self="emit('close')" style="z-index:200">
    <div class="modal" style="width:min(500px,90vw)">
      <div class="modal__head">
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>
        <span>工作流</span>
        <span class="modal__close" @click="emit('close')">&times;</span>
      </div>
      <div class="model-list" style="padding:8px;max-height:50vh;overflow-y:auto">
        <div v-if="loading" style="color:var(--muted-2);text-align:center;padding:20px;font-size:13px">加载中...</div>
        <div v-else-if="templates.length === 0" style="color:var(--muted-2);text-align:center;padding:20px;font-size:13px">暂无工作流模板</div>
        <div v-for="t in templates" :key="t.name" class="model-item" @click="selectWf(t.name)" style="cursor:pointer">
          <div>
            <div class="model-item__title" :style="t.name === 'none' ? { color: 'var(--muted)' } : {}">{{ t.name === 'none' ? '自由对话' : (t.label || t.name) }}</div>
            <div class="model-item__meta">{{ t.name === 'none' ? '灵活模式，自由对话' : (t.description || '') }}</div>
          </div>
          <button class="branch-item__btn" @click.stop="selectWf(t.name)">选择</button>
        </div>
      </div>
    </div>
  </div>
</template>
