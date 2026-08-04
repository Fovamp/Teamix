<script setup lang="ts">
// 消息渲染组件：历史消息与流式消息（streamingMsg）统一使用。
// m: { role: 'user'|'assistant'|'tool', content, reasoning?, _showReasoning?, streaming?, turn? }
import { ref } from "vue"

const props = defineProps<{ m: any; isLatest?: boolean }>()

// 消息级操作（复制/分叉/总结/回溯）：仅当前轮 assistant 消息完成后显示。
// 分叉/总结/回溯为防误触：第一次点击进入确认态，再点执行。
const confirmAction = ref<"fork" | "summary" | "rewind" | null>(null)

// 操作成功后通知会话内容/列表已变化（ChatArea 监听后刷新历史 + 会话列表）
function notifySessionChanged() {
  window.dispatchEvent(new CustomEvent("teamix-session-changed"))
}

async function copyText(text: string) {
  try {
    if (navigator.clipboard?.writeText) await navigator.clipboard.writeText(text)
    else {
      const ta = document.createElement("textarea")
      ta.value = text
      document.body.appendChild(ta)
      ta.select()
      document.execCommand("copy")
      ta.remove()
    }
  } catch { /* ignore */ }
}

function runAction(action: "fork" | "summary" | "rewind") {
  const turn = props.m?.turn
  // turn 缺失或非法（<0）时不发请求（避免 "unavailable for turn -1"）
  if (turn == null || turn < 0) return
  const t = localStorage.getItem("teamix_token")
  if (!t) return
  const q = "?token=" + encodeURIComponent(t)
  const body = action === "fork"
    ? { turn, name: "" }
    : action === "summary"
      ? { turn, mode: "upto" }
      : { turn, scope: "conversation" }
  fetch("/" + (action === "fork" ? "fork" : action === "summary" ? "summarize" : "rewind") + q, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  })
    .then((r) => {
      // 失败不刷新（按钮保留，后端已发错误 notice 显示原因）
      if (r.ok) notifySessionChanged()
    })
    .catch(() => {})
  confirmAction.value = null
}
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
    <!-- 消息级操作（仅当前轮）：复制 / 分叉会话 / 总结 / 回溯（分叉/总结/回溯双击确认防误触） -->
    <div v-if="isLatest" class="msg-actions">
      <button class="msg-actions__btn" title="复制本条回复" @click="copyText(m.content || '')">复制</button>
      <button class="msg-actions__btn" :class="{ 'msg-actions__btn--confirm': confirmAction === 'fork' }"
        @click="confirmAction === 'fork' ? runAction('fork') : confirmAction = 'fork'">
        {{ confirmAction === 'fork' ? '确认分叉？' : '分叉会话' }}
      </button>
      <button class="msg-actions__btn" :class="{ 'msg-actions__btn--confirm': confirmAction === 'summary' }"
        @click="confirmAction === 'summary' ? runAction('summary') : confirmAction = 'summary'">
        {{ confirmAction === 'summary' ? '确认总结？' : '总结' }}
      </button>
      <button class="msg-actions__btn msg-actions__btn--danger" :class="{ 'msg-actions__btn--confirm': confirmAction === 'rewind' }"
        @click="confirmAction === 'rewind' ? runAction('rewind') : confirmAction = 'rewind'">
        {{ confirmAction === 'rewind' ? '确认回溯？' : '回溯' }}
      </button>
    </div>
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
