<script setup lang="ts">
import { ref } from "vue"
const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{ (e: "close"): void }>()
</script>

<template>
<div class="modal-overlay" v-if="visible" @click.self="emit('close')">
  <div class="modal">
    <div class="modal__head"><span>Models</span><span class="modal__close" @click="emit('close')">&times;</span></div>
    <div class="modal__body"><p style="color:var(--muted-2);font-size:13px">加载中...</p></div>
  </div>
</div>
</template>