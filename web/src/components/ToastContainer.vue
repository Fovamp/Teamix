<script setup lang="ts">
import { useToast } from "../composables/useToast"

const { toastState } = useToast()
</script>

<template>
  <div class="toast-container">
    <transition-group name="toast-pop">
      <div v-for="t in toastState.items" :key="t.id" class="toast-item" :class="'toast-item--' + t.type">
        <span class="toast-item__icon">{{ t.type === "success" ? "✓" : t.type === "error" ? "✕" : "ℹ" }}</span>
        <span class="toast-item__msg">{{ t.msg }}</span>
      </div>
    </transition-group>
  </div>
</template>

<style scoped>
.toast-container {
  position: fixed;
  top: 16px;
  right: 16px;
  z-index: 9999;
  display: flex;
  flex-direction: column;
  gap: 8px;
  max-width: min(380px, 90vw);
  pointer-events: none;
}
.toast-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 14px;
  border-radius: 8px;
  font-size: 13px;
  line-height: 1.5;
  box-shadow: var(--shadow-lg);
  border: 1px solid var(--border);
  background: var(--panel);
  color: var(--fg);
  pointer-events: auto;
}
.toast-item__icon {
  width: 18px;
  height: 18px;
  border-radius: 50%;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: 11px;
  flex-shrink: 0;
  color: #000;
}
.toast-item--success .toast-item__icon { background: #4caf50; }
.toast-item--error .toast-item__icon { background: #f44336; }
.toast-item--info .toast-item__icon { background: var(--accent); }
.toast-item--success { border-color: rgba(76, 175, 80, .5); }
.toast-item--error { border-color: rgba(244, 67, 54, .5); }
.toast-pop-enter-active, .toast-pop-leave-active { transition: all .25s ease; }
.toast-pop-enter-from { opacity: 0; transform: translateX(20px); }
.toast-pop-leave-to { opacity: 0; transform: translateX(20px); }
</style>
