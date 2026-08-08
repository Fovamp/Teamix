<script setup lang="ts">
import { ref, onMounted, onUnmounted, nextTick } from "vue"
import { api } from "../api"
import { useToast } from "../composables/useToast"
const { toast } = useToast()

// 运行面板抽屉：右侧悬浮（露出把手 → 点击展开），顶部切换 个人/全局。
// 个人 = 当前用户启动的模块（轮询 status）；全局 = k8s 部署状态（待小工具接入，先占位）。
const open = ref(false)
const view = ref<"personal" | "global">("personal")
const services = ref<any[]>([])
// 展开详情的服务 ID（点击行切换，pre 可选中复制）
const expandedId = ref("")
// 详情实时日志：{ id: { text, offset, timer } }
const svcLogs = ref<Record<string, { text: string; offset: number; timer: any }>>({})
const svcLogEls = ref<Record<string, HTMLElement | null>>({})
function setSvcLogEl(id: string) {
  return (el: HTMLElement | null) => { svcLogEls.value[id] = el }
}
function toggleDetail(id: string) {
  if (expandedId.value === id) {
    expandedId.value = ""
    stopSvcLog(id)
  } else {
    if (expandedId.value) stopSvcLog(expandedId.value)
    expandedId.value = id
    svcLogs.value[id] = { text: "", offset: 0, timer: null }
    fetchSvcLog(id)
    svcLogs.value[id].timer = setInterval(() => fetchSvcLog(id), 1000)
  }
}
function stopSvcLog(id: string) {
  const l = svcLogs.value[id]
  if (l && l.timer) {
    clearInterval(l.timer)
    l.timer = null
  }
}
async function fetchSvcLog(id: string) {
  const l = svcLogs.value[id]
  if (!l) return
  try {
    const res = await api.serviceLog(id, l.offset)
    if (res && res.data) {
      l.text += res.data
      l.offset = res.offset
      nextTick(() => {
        const el = svcLogEls.value[id]
        if (el) el.scrollTop = el.scrollHeight
      })
    } else if (res) {
      l.offset = res.offset
    }
  } catch {}
}
let timer: any = null

// 垂直位置：默认 25%，用户拖动把手调整（只允许上下，避免遮挡）
const posTop = ref("25%")
let dragging = false
let dragged = false // 拖动过（区分"拖动"与"点击"）
let startY = 0, startTop = 0

function onTabDown(e: MouseEvent) {
  dragging = true
  dragged = false
  startY = e.clientY
  const cur = posTop.value.endsWith("%") ? (window.innerHeight * (parseInt(posTop.value, 10) / 100)) : parseInt(posTop.value, 10)
  startTop = Number.isFinite(cur) ? cur : window.innerHeight * 0.25
  document.body.style.cursor = "ns-resize"
  document.body.style.userSelect = "none"
  e.preventDefault()
}
function onMove(e: MouseEvent) {
  if (!dragging) return
  const dy = e.clientY - startY
  if (Math.abs(dy) > 3) dragged = true
  const top = Math.max(40, Math.min(window.innerHeight - 220, startTop + dy))
  posTop.value = top + "px"
}
function onUp() {
  dragging = false
  document.body.style.cursor = ""
  document.body.style.userSelect = ""
}
function onTabClick() {
  if (dragged) return // 刚拖过位置，不算点击
  open.value = !open.value
}

async function refresh() {
  try {
    const list = await api.servicesStatus()
    services.value = list || []
    runningCount.value = services.value.filter((s) => s.stage === "running" || s.stage === "starting").length
  } catch {}
}

function stop(id: string) {
  api.servicesStop(id).then(() => setTimeout(refresh, 400)).catch(() => {})
}

// 重新启动（stopped/failed 状态）：同一项目/模块/端口重新拉起
function restart(s: any) {
  api.servicesStart(s.project, s.service, s.port)
    .then(() => setTimeout(refresh, 400))
    .catch((e: any) => toast(e.message || "启动失败", "error"))
}

function stageLabel(s: string): string {
  if (s === "running") return "运行中"
  if (s === "failed") return "失败"
  if (s === "starting") return "启动中"
  if (s === "stopped") return "已停止"
  return s || "未知"
}

// 悬浮详情：错误 + 最近输出（截断展示）
function detailTitle(s: any): string {
  const parts: string[] = []
  if (s.error) parts.push("错误: " + s.error)
  if (s.output) parts.push("输出:\n" + s.output.slice(-3000))
  return parts.join("\n\n") || s.project + "/" + s.service
}

// 纯退出码（exit status N）不在列表行显示——详情里有完整原因
function isExitCodeOnly(e: string): boolean {
  return /^exit status \d+$/.test((e || "").trim())
}

const runningCount = ref(0)
onMounted(() => {
  refresh()
  timer = setInterval(refresh, 3000)
  window.addEventListener("mousemove", onMove)
  window.addEventListener("mouseup", onUp)
})
onUnmounted(() => {
  if (timer) clearInterval(timer)
  for (const id of Object.keys(svcLogs.value)) stopSvcLog(id)
  window.removeEventListener("mousemove", onMove)
  window.removeEventListener("mouseup", onUp)
})
</script>

<template>
  <div class="svc-drawer" :class="{ 'svc-drawer--open': open }" :style="{ top: posTop }">
    <!-- 常驻把手（露出一条，可上下拖动调整位置） -->
    <div class="svc-drawer__tab"
      :title="open ? '收起运行面板（拖动可调整位置）' : '展开运行面板（拖动可调整位置）'"
      @mousedown="onTabDown" @click="onTabClick">
      <span class="svc-drawer__dot" :class="{ 'svc-drawer__dot--live': runningCount > 0 }"></span>
      <span class="svc-drawer__tab-icon">{{ open ? "▸" : "◂" }}</span>
    </div>

    <!-- 展开面板 -->
    <div v-if="open" class="svc-drawer__panel">
      <div class="svc-drawer__head">
        <span class="svc-drawer__title">运行面板</span>
        <div class="svc-drawer__tabs">
          <button class="svc-drawer__tab-btn" :class="{ active: view === 'personal' }" @click="view = 'personal'">个人</button>
          <button class="svc-drawer__tab-btn" :class="{ active: view === 'global' }" @click="view = 'global'">全局</button>
        </div>
        <span class="svc-drawer__close" @click="open = false">&times;</span>
      </div>
      <div class="svc-drawer__body">
        <template v-if="view === 'personal'">
          <div v-if="services.length === 0" class="svc-drawer__empty">
            暂无运行中的服务<br /><span style="font-size:11px;color:var(--muted-2)">在「选择项目 → 选择模块」中勾选并启动</span>
          </div>
          <template v-for="s in services" :key="s.id">
            <div class="svc-drawer__row" @click="toggleDetail(s.id)">
              <code class="svc-drawer__name">{{ s.project }}/{{ s.service }}</code>
              <span class="svc-drawer__port">:{{ s.port }}</span>
              <span class="svc-drawer__stage" :class="'svc-drawer__stage--' + s.stage">{{ stageLabel(s.stage) }}</span>
              <!-- 启动/停止互相切换：运行中/启动中 → 停止；已停止/失败 → 启动 -->
              <button v-if="s.stage === 'running' || s.stage === 'starting'" class="svc-drawer__stop" @click.stop="stop(s.id)">停止</button>
              <button v-else class="svc-drawer__go" @click.stop="restart(s)">启动</button>
              <span v-if="s.error && s.stage !== 'stopped' && !isExitCodeOnly(s.error)" class="svc-drawer__errline">{{ s.error }}</span>
              <span class="svc-drawer__expand">{{ expandedId === s.id ? "收起" : "详情" }}</span>
            </div>
            <!-- 展开详情：完整实时日志（每秒增量拉取，自动滚到底），pre 可选中复制 -->
            <pre v-if="expandedId === s.id" :ref="setSvcLogEl(s.id)" class="svc-drawer__detail">{{
              (s.error ? "错误: " + s.error + "\n\n" : "") + (svcLogs[s.id]?.text || s.output || "（无输出）")
            }}</pre>
          </template>
        </template>
        <template v-else>
          <!-- 全局视图：k8s 部署状态，待小工具接入真实数据 -->
          <div class="svc-drawer__empty">
            全局部署状态<br /><span style="font-size:11px;color:var(--muted-2)">（k8s 存活数据待接入）</span>
          </div>
        </template>
      </div>
    </div>
  </div>
</template>

<style scoped>
.svc-drawer {
  position: fixed;
  top: 25%;
  right: 0;
  z-index: 150; /* 悬浮于文件树（right panel）之上 */
  display: flex;
  align-items: flex-start;
}
.svc-drawer__tab {
  width: 22px;
  height: 64px;
  border-radius: 8px 0 0 8px;
  background: var(--panel-2);
  border: 1px solid var(--border);
  border-right: none;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 4px;
  cursor: pointer;
  box-shadow: var(--shadow-sm);
  transition: background .15s;
}
.svc-drawer__tab:hover { background: var(--card-hover); }
.svc-drawer__dot {
  width: 8px; height: 8px; border-radius: 50%;
  background: var(--muted-2);
}
.svc-drawer__dot--live { background: var(--success); box-shadow: 0 0 6px var(--success); }
.svc-drawer__tab-icon { font-size: 10px; color: var(--muted-2); }
.svc-drawer__panel {
  width: 400px;
  height: 45vh; /* 约屏幕 1/3~1/2 高度，服务多时 body 内滚动 */
  display: flex;
  flex-direction: column;
  background: var(--panel);
  border: 1px solid var(--border);
  border-right: none;
  border-radius: 10px 0 0 10px;
  box-shadow: var(--shadow-lg);
  overflow: hidden;
}
.svc-drawer__head {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 12px;
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
}
.svc-drawer__title { font-weight: 600; font-size: 13px; }
.svc-drawer__tabs { display: flex; gap: 2px; margin-left: auto; background: var(--bg-2); border-radius: 6px; padding: 2px; }
.svc-drawer__tab-btn {
  padding: 3px 12px; font-size: 12px; border-radius: 5px;
  color: var(--fg-2); cursor: pointer;
}
.svc-drawer__tab-btn.active { background: var(--accent); color: #000; font-weight: 600; }
.svc-drawer__close { margin-left: 4px; font-size: 16px; color: var(--muted-2); cursor: pointer; line-height: 1; }
.svc-drawer__close:hover { color: var(--fg); }
.svc-drawer__body { overflow-y: auto; padding: 8px; min-height: 120px; }
.svc-drawer__empty { color: var(--muted-2); text-align: center; padding: 24px 8px; font-size: 12px; line-height: 1.8; }
.svc-drawer__row {
  display: flex; align-items: center; gap: 6px;
  padding: 6px 8px; margin-bottom: 4px;
  border: 1px solid var(--border); border-radius: 6px;
  background: var(--bg-2); font-size: 12px;
}
.svc-drawer__name { font-size: 12px; flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.svc-drawer__port { color: var(--muted-2); font-size: 11px; }
.svc-drawer__stage { font-size: 10px; padding: 1px 7px; border-radius: 99px; flex-shrink: 0; }
.svc-drawer__stage--running { background: rgba(76,175,80,.15); color: #4caf50; }
.svc-drawer__stage--failed { background: rgba(244,67,54,.16); color: #f44336; }
.svc-drawer__stage--starting { background: rgba(33,150,243,.15); color: #2196f3; }
.svc-drawer__stop {
  padding: 2px 8px; font-size: 11px; flex-shrink: 0;
  border: 1px solid var(--danger); border-radius: 5px;
  background: var(--danger-soft); color: var(--danger); cursor: pointer;
}
.svc-drawer__stop:hover { background: var(--danger); color: #fff; }
.svc-drawer__go {
  padding: 2px 8px; font-size: 11px; flex-shrink: 0;
  border: 1px solid var(--success); border-radius: 5px;
  background: rgba(76,175,80,.12); color: #4caf50; cursor: pointer;
}
.svc-drawer__go:hover { background: var(--success); color: #000; }
.svc-drawer__errline {
  font-size: 10px; color: #f44336; max-width: 90px;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap; flex-shrink: 0;
}
.svc-drawer__expand { font-size: 10px; color: var(--muted-2); flex-shrink: 0; cursor: pointer; }
.svc-drawer__detail {
  margin: 0 0 6px 0; padding: 8px 10px; font-size: 11px; line-height: 1.5;
  background: var(--bg); border: 1px solid var(--border); border-radius: 6px;
  color: var(--fg-2); font-family: var(--mono); white-space: pre-wrap; word-break: break-all;
  max-height: 220px; overflow-y: auto; user-select: text; cursor: text;
}
.svc-drawer__stage--stopped { background: rgba(150,150,150,.15); color: var(--muted-2); }
</style>
