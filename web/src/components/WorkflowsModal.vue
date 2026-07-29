<script setup lang="ts">
const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{ (e: "close"): void }>()
</script>
<template>
  <div class="modal-overlay" v-if="visible" @click.self="emit('close')">
    <div class="modal">
      <div class="modal__head"><span>工作流</span><span class="modal__close" @click="emit('close')">&times;</span></div>
      <div class="modal__body"><p style="color:var(--muted-2);font-size:13px">暂无工作流模板</p></div>
    </div>
  </div>
</template>
