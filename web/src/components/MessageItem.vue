<script setup lang="ts">
// 消息渲染组件：历史消息与流式消息（streamingMsg）统一使用。
// m: { role: 'user'|'assistant'|'tool', content, reasoning?, _showReasoning?, streaming? }
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
  <div v-else-if="m.role === 'tool'" class="msg msg--tool">
    <div class="card" style="margin:0">
      <div class="card-head" style="padding:6px 10px;display:flex;align-items:center;gap:8px;font-size:12px;cursor:pointer;user-select:none" @click="m._open = !m._open" :title="m._open ? '收起输出' : '展开输出'">
        <span style="color:var(--accent);font-weight:500">{{ m.name }}</span>
        <span v-if="m.status === 'running'" style="color:var(--muted-2);font-size:11px">运行中...</span>
        <span v-else-if="m.status === 'error'" style="color:var(--danger);font-size:11px">失败</span>
        <span v-else style="color:var(--ok);font-size:11px">完成</span>
        <span style="margin-left:auto;color:var(--muted-2);font-size:10px;transition:transform .15s" :style="{ transform: m._open ? 'rotate(0deg)' : 'rotate(-90deg)' }">▼</span>
      </div>
      <div v-if="(m.output || m.err) && m._open" class="card-body" style="padding:4px 10px;font-size:11px;max-height:200px;overflow-y:auto;white-space:pre-wrap;background:var(--bg-2);color:var(--fg-2);border-top:1px solid var(--border)">{{ m.err || m.output }}</div>
    </div>
  </div>
</template>
