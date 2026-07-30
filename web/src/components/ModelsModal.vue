<script setup lang="ts">
import { ref, watch } from "vue"
import { api } from "../api"
const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{ (e: "close"): void }>()
const models = ref<any[]>([])
const loading = ref(false)

watch(() => props.visible, async (v) => {
  if (v) {
    loading.value = true
    try {
      const data = await api.models()
      models.value = Array.isArray(data?.models) ? data.models : []
    } catch {
      models.value = []
    }
    loading.value = false
  }
})

async function useModel(refName: string) {
  if (!refName) return
  emit("close")
  try {
    const t = localStorage.getItem('teamix_token')
    if (t) {
      await fetch('/submit?token=' + encodeURIComponent(t), {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ input: '/model ' + refName })
      })
      window.dispatchEvent(new Event("model-changed"))
    }
  } catch {}
}
</script>

<template>
<div class="modal-overlay" v-if="visible" @click.self="emit('close')" style="z-index:200">
  <div class="modal" style="width:min(480px,90vw)">
    <div class="modal__head">
      <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="4" y="4" width="16" height="16" rx="2"/><rect x="9" y="9" width="6" height="6"/><path d="M9 1v3"/><path d="M15 1v3"/><path d="M9 20v3"/><path d="M15 20v3"/><path d="M20 9h3"/><path d="M20 14h3"/><path d="M1 9h3"/><path d="M1 14h3"/></svg>
      <span>模型</span>
      <span class="modal__close" @click="emit('close')">&times;</span>
    </div>
    <div class="model-list" id="models-list" style="padding:8px;max-height:60vh;overflow-y:auto">
      <div v-if="loading" style="color:var(--muted-2);text-align:center;padding:20px;font-size:13px">加载中...</div>
      <div v-else-if="models.length === 0" style="color:var(--muted-2);text-align:center;padding:20px;font-size:13px">加载失败</div>
      <div v-for="m in models" :key="m.ref || m.name"
        class="model-item" :class="{ 'model-item--active': m.active }">
        <div>
          <div class="model-item__title">{{ m.ref || m.name || '' }}</div>
          <div class="model-item__meta">{{ [m.kind, m.default ? 'default' : ''].filter(Boolean).join(' · ') }}</div>
        </div>
        <span v-if="m.active" class="model-item__status model-item__status--active">当前</span>
        <button v-else class="branch-item__btn" @click="useModel(m.ref || m.name)">使用</button>
      </div>
    </div>
  </div>
</div>
</template>
