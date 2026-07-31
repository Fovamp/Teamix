<script setup lang="ts">
// 消息渲染组件：历史消息与流式消息（streamingMsg）统一使用。
// m: { role: 'user'|'assistant', content, reasoning?, _showReasoning?, streaming? }
defineProps<{ m: any }>()
</script>

<template>
  <div v-if="m.role === 'user'" class="msg msg--user">
    <div class="msg__body">
      <div class="msg__text" v-text="m.content"></div>
    </div>
  </div>
  <div v-else-if="m.role === 'assistant'" class="msg msg--assistant">
    <div v-if="m.reasoning" class="reasoning">
      <button class="reasoning__toggle" @click="m._showReasoning = !m._showReasoning">
        <span class="reasoning__chevron" :class="{ 'reasoning__chevron--open': m._showReasoning }">▶</span> 思考过程
      </button>
      <div class="reasoning__body" v-show="m._showReasoning" v-text="m.reasoning"></div>
    </div>
    <span class="msg__text" v-text="m.content"></span><span v-if="m.streaming" class="cursor"></span>
  </div>
</template>
