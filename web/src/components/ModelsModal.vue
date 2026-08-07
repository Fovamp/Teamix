<script setup lang="ts">
import { ref, watch } from "vue"
import { api } from "../api"
import { useToast } from "../composables/useToast"
const { toast } = useToast()
const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{ (e: "close"): void }>()
const models = ref<any[]>([])
const loading = ref(false)
const offline = ref(false)
const saving = ref(false)

watch(() => props.visible, async (v) => {
  if (v) {
    loading.value = true
    try {
      const data = await api.models()
      models.value = Array.isArray(data?.models) ? data.models : []
      offline.value = !!data?.offline
    } catch {
      models.value = []
    }
    loading.value = false
  }
})

async function toggleOffline() {
  saving.value = true
  try {
    const t = localStorage.getItem('teamix_token')
    if (!t) return
    const resp = await fetch('/teamix/offline?token=' + encodeURIComponent(t), {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ offline: !offline.value })
    })
    if (resp.ok) {
      offline.value = !offline.value
    } else {
      const text = await resp.text()
      toast(text || '切换失败（可能仅架构师可操作）')
    }
  } catch {
    toast('切换失败')
  }
  saving.value = false
}

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
      <div style="display:flex;align-items:center;justify-content:space-between;padding:8px 4px;border-bottom:1px solid var(--border, rgba(128,128,128,.2));margin-bottom:6px">
        <div>
          <div style="font-size:13px">仅本地模式</div>
          <div style="font-size:11px;color:var(--muted-2)">全部请求走本地 Qwen，不连接外部模型（架构师可切换）</div>
        </div>
        <button class="branch-item__btn" :disabled="saving" @click="toggleOffline">
          {{ offline ? '关闭仅本地' : '开启仅本地' }}
        </button>
      </div>
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
