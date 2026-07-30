<script setup lang="ts">
import { ref, onMounted, onUnmounted } from "vue"
import { api } from "../api"
const messages = ref<any[]>([])
const inputText = ref("")
const running = ref(false)
const statusText = ref("就绪")

const cwd = ref("-")
const wfName = ref("-")
const wfStages = ref<any[]>([])
const wfVisible = ref(false)
let es: EventSource | null = null
onMounted(() => { loadHistory(); connectSSE(); loadWorkflow(); window.addEventListener("workflow-changed", loadWorkflow) })
onUnmounted(() => { if (es) es.close(); window.removeEventListener("workflow-changed", loadWorkflow) })
async function loadHistory() {
  try { const h = await api.history(); messages.value = (h?.messages || h || []).filter((m: any) => m.role !== "system") } catch (e) { console.error("loadHistory", e) }
  try { const s = await api.status(); running.value = s.running } catch (e) { console.error("loadStatus", e) }
}
function connectSSE() {
  const t = localStorage.getItem("teamix_token")
  if (!t) { console.log("no token for SSE"); return }
  const url = "/events?token=" + encodeURIComponent(t)
  console.log("SSE connecting to", url)
  es = new EventSource(url)
  es.onerror = (err) => { console.error("SSE error", err) }
  es.onerror = function() { if (!running.value) statusText.value = "已断开" }
es.onmessage = function(evt) { console.log("SSE raw:", evt.data); try {
      const e = JSON.parse(evt.data)
      if (e.kind === "text") {
        const last = messages.value[messages.value.length - 1]
        if (last?.role === "assistant" && last.stream) last.content += e.data?.text || ""
        else messages.value.push({ role: "assistant", content: e.data?.text || "", stream: true })
      } else if (e.kind === "reasoning") {
        const last = messages.value[messages.value.length - 1]
        if (last?.role === "assistant") {
          if (!last.reasoning) { last.reasoning = ""; last._showReasoning = false }
          last.reasoning += e.data?.text || ""
        }
      } else if (e.kind === "tool_use") {
        const tool = e.data || {}
        messages.value.push({ role: "tool", id: tool.id, name: tool.name, args: tool.args, status: "running", output: "", open: false, startTime: Date.now() })
      } else if (e.kind === "tool_result") {
        const tool = e.data || {}
        const last = messages.value.slice().reverse().find((m: any) => m.role === "tool" && m.id === tool.id)
        if (last) { last.status = tool.err ? "error" : "done"; last.output = tool.output || "" }
      } else if (e.kind === "turn_end" || e.kind === "text_done") {
        if (messages.value.length) { const last = messages.value[messages.value.length - 1]; if (last) last.stream = false }
        running.value = false; statusText.value = "就绪"
      } else if (e.kind === "turn_start") { running.value = true; statusText.value = "思考中..." }
    } catch (ex) { console.error("SSE parse error", ex, evt.data) }
  }
}
async function loadWorkflow() {
  try { const data = await api.workflow(); if (data && data.stages && data.stages.length > 0) { wfStages.value = data.stages; wfVisible.value = true } } catch {}
}
async function setStage(stage: string) { try { await api.workflowSetStage(stage); await loadWorkflow() } catch {} }


async function sendExample(text: string) {
  inputText.value = text
  await send()
}

async function send() {
  const text = inputText.value.trim()
  if (!text) return
  messages.value.push({ role: "user", content: text })
  inputText.value = ""
  running.value = true; statusText.value = "思考中..."
  try {
    console.log("sending:", text)
    await api.submit(text)
    console.log("submit ok")
  } catch (e: any) {
    console.error("submit error", e)
    running.value = false; statusText.value = "就绪"
  }
}
async function doStop() { await api.cancel(); running.value = false; statusText.value = "已取消" }
</script>
<template>
  <section class="transcript" id="log">
    <div v-if="messages.length === 0" class="welcome" id="welcome">
      <div class="welcome__brand"><svg width="240" height="56" viewBox="0 0 240 56"><text x="20" y="40" font-size="32" font-weight="700" fill="var(--fg)">Teamix</text><text x="140" y="40" font-size="20" fill="var(--accent)" font-weight="600">Cloud</text></svg></div>
      <div class="welcome__tag">AI 协作开发平台</div>
      <div class="welcome__meta">
        <div class="welcome__pill"><strong>模型</strong><span id="welcome-model">{{ statusModel }}</span></div>
        <div class="welcome__pill"><strong>工作区</strong><span id="welcome-cwd" class="welcome-cwd">{{ cwd }}</span></div>
        <div class="welcome__pill"><strong>工作流</strong><span id="welcome-wf">{{ wfName }}</span></div>
      </div>
      <div class="welcome__hints">
        <span><kbd>/</kbd> 命令</span>
        <span><kbd>Shift+Tab</kbd> 计划</span>
        <span><kbd>Ctrl+Y</kbd> YOLO</span>
        <span><kbd>Esc Esc</kbd> 回退</span>
      </div>
      <div class="welcome__examples">
        <button class="welcome__ex" @click="sendExample('解释代码')">解释代码</button>
        <button class="welcome__ex" @click="sendExample('修复bug')">修复 bug</button>
        <button class="welcome__ex" @click="sendExample('写测试')">写测试</button>
      </div>
    </div>
    <div v-for="(m, i) in messages" :key="i">
      <div v-if="m.role === 'user'" class="msg msg--user">
        <span class="msg__caret">&#8250;</span>
        <div class="msg__text">{{ m.content }}</div>
      </div>
      <div v-else-if="m.role === 'assistant'" class="msg msg--assistant">
        <div v-if="m.reasoning" class="reasoning">
          <button class="reasoning__toggle" @click="m._showReasoning = !m._showReasoning">
            <span class="reasoning__chevron" :class="{ 'reasoning__chevron--open': m._showReasoning }">&#9654;</span> 思考过程
          </button>
          <div class="reasoning__body" v-show="m._showReasoning">{{ m.reasoning }}</div>
        </div>
        <span class="msg__text">{{ m.content }}<span v-if="m.stream" class="cursor"></span></span>
      </div>
      <div v-else-if="m.role === 'tool'" class="card" :class="{ 'card--done': m.status === 'done', 'card--err': m.status === 'error' }">
        <div class="card-head" @click="m.open = !m.open">
          <span class="ico" :class="{ spin: m.status === 'running' }">&#9679;</span>
          <div class="card-main">
            <div class="card-title"><span class="name">{{ m.name }}</span><span v-if="m.args" class="subject">{{ typeof m.args === 'string' ? m.args.substring(0,90) : JSON.stringify(m.args).substring(0,90) }}</span></div>
            <div class="card-meta">{{ m.args ? (JSON.stringify(m.args).length / 1000).toFixed(1) + 'k' : '' }}</div>
          </div>
          <span class="card-badge">{{ { running: '运行中', done: '完成', error: '失败' }[m.status] || m.status }}</span>
          <div class="card-actions"><span class="chev">&#9660;</span></div>
        </div>
        <div class="card-body" v-show="m.open">{{ m.output || '无输出' }}</div>
      </div>
    </div>
  </section>
  <footer class="footer">
    <div class="toolbar">
      <button class="toolbar__btn toolbar__btn--ok" id="btn-auto" title="自动模式"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><path d="M8 12l2 2 4-4"/></svg> 自动</button>
      <button class="toolbar__btn" id="btn-plan" title="计划模式"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M2 3h6a4 4 0 0 1 4 4v14a3 3 0 0 0-3-3H2z"/><path d="M22 3h-6a4 4 0 0 0-4 4v14a3 3 0 0 1 3-3h7z"/></svg> 计划</button>
      <button class="toolbar__btn" id="btn-bypass" title="YOLO 模式"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z"/></svg> YOLO</button>
      <button class="toolbar__btn" id="btn-goal" title="目标模式"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><circle cx="12" cy="12" r="6"/><line x1="12" y1="2" x2="12" y2="4"/><line x1="12" y1="20" x2="12" y2="22"/><line x1="2" y1="12" x2="4" y2="12"/><line x1="20" y1="12" x2="22" y2="12"/></svg> 目标</button>
      <div class="toolbar__sep"></div>
      <div class="status"><span class="status__dot" id="status-dot-footer" :class="{ 'status__dot--busy': running }"></span><span id="status-text">{{ statusText }}</span></div>
      <div class="toolbar__spacer"></div>
      <div class="wf-bar" id="wf-bar" v-if="wfVisible">
        <div v-for="(s, i) in wfStages" :key="s.stage || i" class="wf-step" @click="setStage(s.stage)" title="切换到此阶段">
          <span class="wf-dot" :class="{ 'wf-dot--active': s.status === 'in_progress', 'wf-dot--done': s.status === 'completed' }"></span>
          <span class="wf-label" :class="{ 'wf-label--active': s.status === 'in_progress', 'wf-label--done': s.status === 'completed' }">{{ s.label || s.stage }}</span>
          <div class="wf-line" v-if="i < wfStages.length - 1"></div>
        </div>
      </div>
      <div class="status" id="turn-info"></div>
    </div>
    <div class="composer">
      <span class="composer__caret">&#8250;</span>
      <textarea v-model="inputText" class="composer__input" id="in" placeholder="输入消息..." rows="1" @keydown.enter.prevent="send"></textarea>
      <button class="composer__btn composer__btn--send" id="btn-send" title="发送" v-show="!running" @click="send"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" width="18" height="18"><line x1="12" y1="19" x2="12" y2="5"/><polyline points="5 12 12 5 19 12"/></svg></button>
      <button class="composer__btn composer__btn--stop" id="btn-stop" title="取消" v-show="running" @click="doStop"><svg viewBox="0 0 24 24" fill="currentColor" width="18" height="18"><rect x="6" y="6" width="12" height="12" rx="2"/></svg></button>
    </div>
  </footer>
</template>
