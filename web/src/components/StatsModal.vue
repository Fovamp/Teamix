<script setup lang="ts">
import { ref, watch } from "vue"
import { api } from "../api"
const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{ (e: "close"): void }>()
const status = ref<any>(null)

watch(() => props.visible, async (v) => {
  if (v) {
    try {
      const s = await api.status()
      status.value = s
      // Also get cumulative stats from window event
      const cumulative = (window as any)._cumulativeStats
      if (cumulative) {
        s.totalCost = cumulative.cost || s.totalCost
        s.totalTokens = cumulative.tokens || s.totalTokens
      }
    } catch {}
  }
})
</script>

<template>
  <div class="modal-overlay" v-if="visible" @click.self="emit('close')">
    <div class="modal">
      <div class="modal__head">
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 20V10"/><path d="M12 20V4"/><path d="M6 20v-6"/></svg>
        <span>统计</span>
        <span class="modal__close" @click="emit('close')">&times;</span>
      </div>
      <div class="modal__body">
        <div class="stat-grid">
          <div class="stat-card"><div class="stat-card__label">模型</div><div class="stat-card__value" id="stats-model">{{ status?.label || "-" }}</div></div>
          <div class="stat-card"><div class="stat-card__label">会话</div><div class="stat-card__value" id="stats-sessions">{{ status?.sessions || 0 }}</div></div>
          <div class="stat-card"><div class="stat-card__label">总 Token</div><div class="stat-card__value acc" id="stats-total-tokens">{{ status?.totalTokens || 0 }}</div></div>
          <div class="stat-card"><div class="stat-card__label">缓存命中率</div><div class="stat-card__value ok" id="stats-cache-rate">{{ status?.cacheHitRate || "0%" }}</div></div>
          <div class="stat-card"><div class="stat-card__label">会话费用</div><div class="stat-card__value" id="stats-total-cost">{{ typeof status?.totalCost === "number" ? "$" + status.totalCost.toFixed(4) : "-" }}</div></div>
          <div class="stat-card"><div class="stat-card__label">余额</div><div class="stat-card__value" id="stats-balance">{{ status?.balance?.display || "-" }}</div></div>
          <div class="stat-card stat-card--wide">
            <div class="stat-card__label">上下文用量</div>
            <div class="ctx-bar" style="margin-top:8px"><div class="ctx-bar__fill" id="stats-ctx-fill" :style="{ width: (status?.window ? Math.round(status.used/status.window*100) : 0) + '%' }"></div></div>
            <div class="ctx-label" style="margin-top:4px"><span id="stats-ctx-used">{{ status?.used || 0 }} tok</span><span id="stats-ctx-window">{{ status?.window || 0 }} tok</span></div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
