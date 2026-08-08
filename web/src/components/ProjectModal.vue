<script setup lang="ts">
import { ref, watch } from "vue"
import { api } from "../api"
import { useToast } from "../composables/useToast"

const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{ (e: "close"): void; (e: "selected"): void }>()
const { toast } = useToast()

const projects = ref<any[]>([])
const currentProject = ref("")
const loading = ref(false)
const err = ref("")
const working = ref(false)

// clone 进度条（选择项目时真实百分比，轮询 /teamix/clone/progress）
const cloneActive = ref(false)
const cloneProgress = ref("")
let pollTimer: any = null
function startPolling(project: string) {
  stopPolling()
  pollTimer = setInterval(async () => {
    try {
      const p = await api.cloneProgress(project)
      if (p && p.progress) cloneProgress.value = p.progress
    } catch {}
  }, 1200)
}
function stopPolling() {
  if (pollTimer) {
    clearInterval(pollTimer)
    pollTimer = null
  }
}
// "45% (1234/2742)" → 45（进度条宽度）
const clonePercent = ref(0)
function updatePercent() {
  const m = /(\d+)%/.exec(cloneProgress.value)
  clonePercent.value = m ? parseInt(m[1], 10) : 0
}

watch(cloneProgress, updatePercent)

// 模块选择（资源池）：按项目多选 + 映射端口，缓存按用户隔离。
// selectedByProject: { project: { module: 映射端口 } }
const selectedByProject = ref<Record<string, Record<string, number>>>({})
const svcStoreKey = () => "teamix_selected_services_" + (api.currentUser() || "default")
const showModule = ref(false)
const moduleProject = ref("")
const moduleServices = ref<any[]>([])
const moduleLoading = ref(false)
const moduleSel = ref<string[]>([])
// 映射端口：module -> 端口（勾选时显示输入框）；建议端口只读预览：module -> 建议值
const modulePorts = ref<Record<string, string>>({})
const moduleSuggest = ref<Record<string, string>>({})
const moduleConflicts = ref<Record<string, string>>({})
// 启动状态（项目就绪后自动启动所选模块，轮询 status 显示阶段）
const svcStarting = ref(false)
// 启动状态区展开详情的模块名（点击"详情"切换，pre 可复制）
const expandedSvc = ref("")
function toggleSvcDetail(m: string) {
  expandedSvc.value = expandedSvc.value === m ? "" : m
}
const svcStatusRows = ref<Record<string, any>>({})
let svcPollTimer: any = null

// 凭证步骤
const credStep = ref(false)
const targetProject = ref("")
const credMode = ref<"ssh" | "https">("ssh")
const sshKeyPath = ref("")
const httpsUser = ref("")
const httpsPass = ref("")
const credErr = ref("")
const configured = ref(false)

async function load() {
  loading.value = true
  err.value = ""
  try {
    const [ps, st, creds] = await Promise.all([
      api.projects(),
      api.status().catch(() => null),
      api.gitCredentials().catch(() => null),
    ])
    projects.value = ps || []
    // 不回显上次选中的项目（每次打开都是初始状态）
    try {
      const raw = JSON.parse(localStorage.getItem(svcStoreKey()) || "{}")
      // 旧格式 {project: ["mod"]} → 新格式 {project: {mod: 端口}}
      for (const [proj, v] of Object.entries(raw)) {
        if (Array.isArray(v)) {
          const m: Record<string, number> = {}
          for (const name of v as string[]) m[name] = 0
          ;(raw as any)[proj] = m
        }
      }
      selectedByProject.value = raw
    } catch {
      selectedByProject.value = {}
    }
    if (creds) {
      configured.value = !!creds.configured
      sshKeyPath.value = creds.sshKeyPath || ""
      httpsUser.value = creds.httpsUsername || ""
      if (creds.sshKeyPath) credMode.value = "ssh"
      else if (creds.httpsUsername) credMode.value = "https"
    }
  } catch (e: any) {
    err.value = e.message || "加载失败"
  } finally {
    loading.value = false
  }
}

watch(() => props.visible, (v) => {
  if (v) {
    // 每次打开重置为初始状态（不回显上次操作）
    err.value = ""
    working.value = false
    credStep.value = false
    targetProject.value = ""
    credErr.value = ""
    currentProject.value = ""
    load()
    // 刷新页面后恢复：检测是否有进行中的 clone（后端 continue）
    checkRunningClone()
  }
})

async function checkRunningClone() {
  try {
    const p = await api.cloneProgress()
    if (p && p.running && p.project) {
      cloneActive.value = true
      cloneProgress.value = p.progress || ""
      startPolling(p.project)
      toast("检测到进行中的项目拉取，正在继续...", "info", 10000)
    }
  } catch {}
}

// 同步勾选模块：先校验端口（冲突 → 打开模块弹窗标红，不启动），
// 通过后 sync 启动/关闭，轮询 status 显示每模块阶段。
// 注意：sync 是全量同步（不在勾选集合的运行服务会被关闭）——必须传全部项目的
// 勾选 items，否则切到项目 B 时项目 A 的服务会被静默 kill。
// 返回 true = 已发起启动；false = 冲突/失败（调用方不关闭弹窗）。
async function syncSelectedModules(project: string): Promise<boolean> {
  const allItems = Object.entries(selectedByProject.value).flatMap(([proj, mods]) =>
    Object.entries(mods).filter(([, p]) => p > 0).map(([module, port]) => ({ project: proj, module, port }))
  )
  if (allItems.length === 0) return true
  if (!(await checkPortConflicts(allItems))) {
    // 冲突 → 回到模块选择弹窗让用户改端口（保留已写入的标红）
    await openModuleModal(project, true)
    toast("所选模块端口冲突，请在模块选择中修改后重试", "error")
    return false
  }
  svcStarting.value = true
  svcStatusRows.value = {}
  try {
    const sr = await api.servicesSync(allItems)
    // sync 可能整体拒绝（TOCTOU：validate 与 sync 之间端口被占）→ 回填冲突
    if (sr && sr.ok === false && sr.conflicts) {
      const conflicts: Record<string, string> = {}
      for (const [k, reason] of Object.entries(sr.conflicts)) {
        const module = k.includes("/") ? k.split("/").pop()! : k
        conflicts[module] = reason
      }
      moduleConflicts.value = conflicts
      await openModuleModal(project, true)
      toast("启动时端口冲突，请修改后重试", "error")
      return false
    }
    // startService 失败项（如 Maven 缺失）→ 直显 failed + 不关闭弹窗（让用户看到原因后手动关）
    const failedRows: Record<string, any> = {}
    for (const res of (sr && sr.results) || []) {
      if (res.action === "failed") {
        failedRows[res.module] = { service: res.module, port: res.port, stage: "failed", error: res.error || "启动失败" }
      }
    }
    if (Object.keys(failedRows).length) {
      svcStatusRows.value = { ...svcStatusRows.value, ...failedRows }
      toast("部分模块启动失败，请查看下方错误详情", "error")
      return false // 不关闭弹窗，错误信息停留（svcStarting 区保留 rows 显示）
    }
    await pollSvcStatus(project)
    // 轮询结束后：有 failed → 不关闭弹窗，让用户看错误（rows 停留显示）
    const anyFailed = Object.entries(selectedByProject.value[project] || {})
      .filter(([, p]) => p > 0)
      .some(([m]) => svcStatusRows.value[m] && svcStatusRows.value[m].stage === "failed")
    if (anyFailed) {
      toast("部分模块启动失败，请查看下方错误详情", "error")
      return false
    }
    const stillStarting = Object.values(svcStatusRows.value).some((s: any) => s && s.stage === "starting")
    if (stillStarting) toast("部分模块仍在后台启动中（首次下载依赖较慢），可在运行面板查看", "info", 8000)
  } catch (e: any) {
    toast("模块启动失败: " + (e.message || e), "error")
    return false
  } finally {
    svcStarting.value = false
    stopSvcPolling()
  }
  return true
}

function pollSvcStatus(project: string) {
  stopSvcPolling()
  return new Promise<void>((resolve) => {
    let tries = 0
    svcPollTimer = setInterval(async () => {
      tries++
      try {
        const list = await api.servicesStatus()
        // 保留已有 failed 直显行（服务端 failed 后即 remove，status 不再返回；
        // 若被覆盖则失败项 2s 后消失、前端又空等）
        const rows: Record<string, any> = { ...svcStatusRows.value }
        for (const s of list) {
          if (s.project === project) rows[s.service] = s
        }
        svcStatusRows.value = rows
        // 只等"勾选且端口>0"的模块（sync 只启动这些）
        const want = Object.entries(selectedByProject.value[project] || {})
          .filter(([, p]) => p > 0)
          .map(([m]) => m)
        const allDone = want.length > 0 && want.every((m) => {
          const s = rows[m]
          return s && (s.stage === "running" || s.stage === "failed")
        })
        if (allDone || tries > 60) resolve() // 60 次 ≈ 2 分钟兜底
      } catch {}
    }, 2000)
  })
}
function stopSvcPolling() {
  if (svcPollTimer) {
    clearInterval(svcPollTimer)
    svcPollTimer = null
  }
}

async function doSelect(project: string) {
  working.value = true
  credErr.value = ""
  err.value = ""
  // 本地已有（已 clone）→ 直接连接，不弹拉取提示；否则进 clone 进度流程。
  const proj = projects.value.find((x: any) => x.name === project)
  const needClone = !(proj && proj.cloned)
  if (needClone) {
    cloneActive.value = true
    cloneProgress.value = ""
    startPolling(project)
    // 大仓库克隆可能较慢：明确告知进行中
    toast("正在拉取项目代码（大仓库可能需要几分钟）...", "info", 60000)
  } else {
    toast("正在连接项目...", "info", 5000)
  }
  try {
    const r = await api.projectSelect(project)
    stopPolling()
    cloneActive.value = false
    if (r && r.needCredentials) {
      targetProject.value = project
      credStep.value = true
      credErr.value = (r && r.error) || ""
      toast("需要配置 git 凭证：" + ((r && r.error) || ""), "error")
      return
    }
    if (r && r.ok) {
      currentProject.value = project
      // 自动启动勾选模块（sync：勾了没跑→启动/在跑→不动/没勾在跑→关闭）；
      // 端口冲突时返回 false，弹窗不关、回到模块选择改端口
      const ok = await syncSelectedModules(project)
      if (!ok) return
      toast("项目已就绪", "success")
      emit("selected")
      emit("close")
      return
    }
    const failMsg = (r && r.error) || "选择项目失败"
    err.value = failMsg
    toast(failMsg, "error")
  } catch (e: any) {
    const msg = e.message || "选择项目失败"
    err.value = msg
    toast(msg, "error")
  } finally {
    working.value = false
  }
}

async function saveCredentials() {
  working.value = true
  credErr.value = ""
  try {
    // 前端先校验，避免"只填令牌没填用户名"导致保存静默失败后再弹表单
    if (credMode.value === "https" && !httpsUser.value.trim()) {
      credErr.value = "请填写用户名（令牌填在密码/Token 栏；用户名可填 oauth2 或任意非空值）"
      return
    }
    const body = credMode.value === "ssh"
      ? { sshKeyPath: sshKeyPath.value.trim() }
      : { httpsUsername: httpsUser.value.trim(), httpsPassword: httpsPass.value }
    const r = await api.gitCredentialsSave(body)
    if (r && r.ok === false) {
      credErr.value = r.error || "凭证校验失败"
      return
    }
    // 保存成功 → 继续选择目标项目
    if (targetProject.value) {
      credStep.value = false
      await doSelect(targetProject.value)
    }
  } catch (e: any) {
    credErr.value = e.message || "保存凭证失败"
  } finally {
    working.value = false
  }
}

function close() {
  if (working.value || cloneActive.value || svcStarting.value) {
    toast("项目拉取/模块启动进行中，请等待完成", "info")
    return
  }
  stopSvcPolling() // 关闭弹窗停止启动状态轮询（模块仍在后台运行）
  emit("close")
}

// 模块选择（独立二级模态窗，多选假选择，为资源池预留）
async function openModuleModal(project: string, keepConflicts = false) {
  moduleProject.value = project
  // 回显已存映射端口（旧 string[] 格式兼容为 0）
  const saved = selectedByProject.value[project] || {}
  moduleSel.value = Object.keys(saved)
  const ports: Record<string, string> = {}
  for (const [m, p] of Object.entries(saved)) ports[m] = p ? String(p) : ""
  modulePorts.value = ports
  // 冲突回弹窗（sync 失败路径）时保留已写入的标红；正常打开/切换项目则清空
  if (!keepConflicts) moduleConflicts.value = {}
  showModule.value = true
  moduleLoading.value = true
  moduleServices.value = []
  try {
    moduleServices.value = await api.projectServices(project).catch(() => [])
  } finally {
    moduleLoading.value = false
  }
}

function toggleModule(name: string) {
  const i = moduleSel.value.indexOf(name)
  if (i >= 0) {
    moduleSel.value.splice(i, 1)
    const ports = { ...modulePorts.value }
    delete ports[name]
    modulePorts.value = ports
    const sugg = { ...moduleSuggest.value }
    delete sugg[name]
    moduleSuggest.value = sugg
  } else {
    moduleSel.value.push(name)
    // 建议映射端口只读预览：自身端口 1000-9999 时前缀 1（8081 → 18081）；
    // 5 位端口（≥10000）前缀 1 会超 65535，不生成建议，仅提示用户自行填写
    const svc = moduleServices.value.find((s: any) => s.name === name)
    const selfPort = svc && svc.port ? svc.port : 0
    const suggest = selfPort >= 1000 && selfPort <= 9999 ? "1" + selfPort : ""
    moduleSuggest.value = { ...moduleSuggest.value, [name]: suggest }
  }
  // 端口改动后清除该模块冲突标记
  const c = { ...moduleConflicts.value }
  delete c[name]
  moduleConflicts.value = c
}

function closeModule() {
  if (moduleLoading.value) return
  showModule.value = false
}

// 确定：先校验映射端口（冲突标红、弹窗不关、改端口重试），通过后保存。
async function confirmModule() {
  const items = moduleSel.value
    .filter((m) => modulePorts.value[m])
    .map((m) => ({ project: moduleProject.value, module: m, port: parseInt(modulePorts.value[m], 10) || 0 }))
  if (items.length === 0) {
    moduleConflicts.value = Object.fromEntries(moduleSel.value.map((m) => [m, "port_required"]))
    toast("请为选中的模块填写映射端口")
    return
  }
  if (!(await checkPortConflicts(items))) return // 冲突标红、弹窗不关
  // 通过 → 保存（module -> 端口）
  const saved: Record<string, number> = {}
  for (const it of items) saved[it.module] = it.port
  selectedByProject.value[moduleProject.value] = saved
  localStorage.setItem(svcStoreKey(), JSON.stringify(selectedByProject.value))
  moduleConflicts.value = {}
  showModule.value = false
  toast("模块已选择，点击项目卡片开始拉取并启动", "success")
}

// checkPortConflicts 调服务端校验；有冲突 → 写入 moduleConflicts（key 归一为模块名）并返回 false。
async function checkPortConflicts(items: Array<{ project: string; module: string; port: number }>): Promise<boolean> {
  try {
    const r = await api.servicesValidate(items)
    const raw = (r && r.conflicts) || {}
    const conflicts: Record<string, string> = {}
    for (const [k, reason] of Object.entries(raw)) {
      const module = k.includes("/") ? k.split("/").pop()! : k
      conflicts[module] = reason
    }
    if (Object.keys(conflicts).length > 0) {
      moduleConflicts.value = conflicts
      toast("端口冲突，请修改后重试", "error")
      return false
    }
    return true
  } catch (e: any) {
    toast("校验失败: " + (e.message || e), "error")
    return false
  }
}

function projectSelected(name: string) {
  return Object.keys(selectedByProject.value[name] || {})
}
// 端口输入后清除该模块冲突标记
function clearConflict(name: string) {
  const c = { ...moduleConflicts.value }
  delete c[name]
  moduleConflicts.value = c
}
// 冲突原因中文
function conflictText(reason: string): string {
  if (reason === "port_inuse") return "端口已被占用"
  if (reason === "port_required") return "请填写映射端口"
  if (reason === "port_invalid") return "端口无效（1-65535）"
  if (reason.startsWith("dup_port:")) return "与 " + reason.slice(9) + " 映射端口重复"
  return reason
}
// 阶段中文
function stageLabel(s: string): string {
  if (s === "running") return "运行中"
  if (s === "failed") return "启动失败"
  if (s === "starting") return "启动中（下载依赖/编译）"
  return s || "未知"
}
</script>

<template>
  <div class="modal-overlay" v-if="visible" @click.self="close" style="z-index:200">
    <div class="modal" style="width:min(560px,92vw);max-height:75vh;display:flex;flex-direction:column">
      <div class="modal__head" style="flex-shrink:0">
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M3 7v10l7 4V11z"/><path d="M3 7l7-4 7 4-7 4z"/><path d="M17 7v10"/><path d="M10 11v10"/><path d="M17 11h4v6h-4"/></svg>
        <span>选择项目</span>
        <span class="modal__close" @click="close">&times;</span>
      </div>

      <!-- 醒目临时提示（全局 ToastContainer 渲染） -->

      <!-- clone 进度条 -->
      <div v-if="cloneActive" class="clone-progress">
        <div class="clone-progress__track">
          <div class="clone-progress__fill" :style="{ width: clonePercent + '%' }"></div>
        </div>
        <span class="clone-progress__text">{{ cloneProgress ? ("正在拉取项目代码 " + cloneProgress) : "正在准备拉取..." }}</span>
      </div>

      <div style="flex:1;min-height:0;overflow-y:auto;padding:12px">
        <div v-if="loading" style="color:var(--muted-2);text-align:center;padding:24px;font-size:13px">加载项目列表...</div>
        <div v-else-if="err" style="color:var(--danger);padding:12px;font-size:13px">{{ err }}</div>

        <!-- 项目列表 -->
        <template v-else>
          <div v-if="projects.length === 0" style="color:var(--muted-2);text-align:center;padding:24px;font-size:13px">
            暂无可用项目（请架构师在 .teamix/projects.yaml 中配置）
          </div>
          <div v-for="p in projects" :key="p.name" class="proj-card" @click="working ? null : doSelect(p.name)"
            :class="{ 'proj-card--active': p.name === currentProject, 'proj-card--working': working }">
            <div class="proj-card__main">
              <div class="proj-card__name">{{ p.name }}
                <span v-if="p.name === currentProject" class="proj-card__cur">当前</span>
              </div>
              <div class="proj-card__desc">{{ p.description || "（无描述）" }}</div>
            </div>
            <div class="proj-card__meta">
              <span>{{ p.serviceCount }} 个服务</span>
              <span v-if="p.git" class="proj-card__git">{{ p.git }}</span>
              <button class="proj-card__expand" @click.stop="openModuleModal(p.name)">
                选模块<span v-if="projectSelected(p.name).length" class="proj-card__sel-n">({{ projectSelected(p.name).length }})</span>
              </button>
            </div>
            <div v-if="projectSelected(p.name).length" class="proj-card__chips">
              <span v-for="s in projectSelected(p.name).slice(0, 3)" :key="s" class="proj-card__chip">{{ s }}</span>
              <span v-if="projectSelected(p.name).length > 3" class="proj-card__chip proj-card__chip--more">+{{ projectSelected(p.name).length - 3 }}</span>
            </div>
          </div>
        </template>

        <!-- 模块启动状态（选择项目后自动启动；有记录就一直显示，失败信息停留可看） -->
        <div v-if="svcStarting || Object.keys(svcStatusRows).length > 0" class="svc-starting">
          <div style="font-size:12px;font-weight:600;margin-bottom:6px">正在启动所选模块（下载依赖/编译/启动中...）</div>
          <template v-for="(s, m) in svcStatusRows" :key="m">
            <div class="svc-starting__row">
              <code>{{ m }}</code>
              <span v-if="s.port" class="svc-starting__port">:{{ s.port }}</span>
              <span class="svc-starting__stage" :class="'svc-starting__stage--' + s.stage">{{ stageLabel(s.stage) }}</span>
              <span v-if="s.error" class="svc-starting__detail" @click="toggleSvcDetail(m)">{{ expandedSvc === m ? "收起" : "详情" }}</span>
            </div>
            <pre v-if="expandedSvc === m" class="svc-starting__log">{{
              (s.error ? "错误: " + s.error + "\n\n" : "") + (s.output || "（无输出）")
            }}</pre>
          </template>
          <div v-if="Object.keys(svcStatusRows).length === 0" style="color:var(--muted-2);font-size:12px">等待启动...</div>
        </div>

        <!-- 凭证表单 -->
        <div v-if="credStep" class="cred-box">
          <div class="cred-box__title">配置 Git 凭证（{{ targetProject }}）</div>
          <div class="cred-box__mode">
            <button class="cred-mode-btn" :class="{ active: credMode === 'ssh' }" @click="credMode = 'ssh'">SSH Key</button>
            <button class="cred-mode-btn" :class="{ active: credMode === 'https' }" @click="credMode = 'https'">HTTPS 账号</button>
          </div>

          <div v-if="credMode === 'ssh'" class="cred-box__row">
            <label>SSH 私钥路径</label>
            <input v-model="sshKeyPath" type="text" placeholder="C:\Users\you\.ssh\id_ed25519 或 ~/.ssh/id_ed25519" />
          </div>
          <template v-else>
            <div class="cred-box__row">
              <label>用户名</label>
              <input v-model="httpsUser" type="text" placeholder="git 用户名" />
            </div>
            <div class="cred-box__row">
              <label>密码 / Token</label>
              <input v-model="httpsPass" type="password" placeholder="密码或个人访问令牌" />
            </div>
          </template>

          <div v-if="credErr" class="cred-box__err">{{ credErr }}</div>
          <div class="cred-box__actions">
            <button class="btn" @click="credStep = false">取消</button>
            <button class="btn primary" :disabled="working" @click="saveCredentials">
              {{ working ? "处理中..." : "保存并选择项目" }}
            </button>
          </div>
        </div>
      </div>
    </div>
  </div>

  <!-- 二级模态窗：选择模块 -->
  <div class="modal-overlay" v-if="showModule" @click.self="closeModule" style="z-index:300">
    <div class="modal" style="width:min(520px,90vw);max-height:70vh;display:flex;flex-direction:column">
      <div class="modal__head" style="flex-shrink:0">
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/></svg>
        <span>选择模块 - {{ moduleProject }}</span>
        <span class="modal__close" @click="closeModule">&times;</span>
      </div>
      <div style="flex:1;min-height:0;overflow-y:auto;padding:12px">
        <div v-if="moduleLoading" style="color:var(--muted-2);text-align:center;padding:24px;font-size:13px">加载模块...</div>
        <template v-else>
          <div v-if="moduleServices.length === 0" style="color:var(--muted-2);text-align:center;padding:24px;font-size:13px">该项目未配置模块</div>
          <div v-for="s in moduleServices" :key="s.name" class="proj-card__svc"
            :class="{ 'proj-card__svc--sel': moduleSel.includes(s.name) }" @click="toggleModule(s.name)">
            <span class="proj-card__svc-check">{{ moduleSel.includes(s.name) ? "✓" : "" }}</span>
            <span class="proj-card__svc-name">{{ s.name }}</span>
            <span class="proj-card__svc-type">{{ s.type }}</span>
            <span v-if="s.port" class="proj-card__svc-port">自身 :{{ s.port }}</span>
            <!-- 勾选后才显示映射端口输入框（建议端口放 placeholder，输入框由用户填写） -->
            <template v-if="moduleSel.includes(s.name)">
              <span class="proj-card__svc-arrow">→</span>
              <input v-model="modulePorts[s.name]" @click.stop @input="clearConflict(s.name)"
                class="proj-card__svc-input" type="text" inputmode="numeric"
                :placeholder="moduleSuggest[s.name] ? ('建议 ' + moduleSuggest[s.name]) : '映射端口'"
                @keyup.enter="confirmModule"
                :class="{ 'proj-card__svc-input--err': moduleConflicts[s.name] }" />
            </template>
          </div>
          <div v-if="Object.keys(moduleConflicts).length" style="margin-top:6px;font-size:12px;color:#f44336">
            <div v-for="(reason, m) in moduleConflicts" :key="m">{{ m }}：{{ conflictText(reason) }}</div>
            <div style="color:var(--muted-2);margin-top:2px">请修改映射端口后重新确认（冲突未解决前不会启动）</div>
          </div>
          <div style="margin-top:12px;display:flex;gap:8px;justify-content:center">
            <button class="btn" @click="closeModule">取消</button>
            <button class="btn primary" :disabled="moduleSel.length === 0" @click="confirmModule">
              确认并校验端口（{{ moduleSel.length }}）
            </button>
          </div>
        </template>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* clone 进度条（modal 顶部） */
.clone-progress { padding: 10px 16px; border-bottom: 1px solid var(--border); }
.clone-progress__track { height: 6px; border-radius: 99px; background: var(--bg-2); overflow: hidden; }
.clone-progress__fill { height: 100%; border-radius: 99px; background: var(--accent); transition: width .3s ease; }
.clone-progress__text { display: block; margin-top: 6px; font-size: 12px; color: var(--muted-2); font-family: var(--mono); }
.btn { padding: 6px 14px; border: 1px solid var(--border); border-radius: 6px; background: var(--bg); color: var(--fg-2); font-size: 12px; cursor: pointer; transition: all .12s; }
.btn:hover { background: var(--bg-2); color: var(--fg); }
.btn.primary { border: none; background: var(--accent); color: #000; font-weight: 600; }
.btn.primary:hover { background: var(--accent-strong); color: #000; }
.btn.primary:disabled { opacity: .6; cursor: not-allowed; }
.btn:disabled:hover { opacity: .6; cursor: not-allowed; }
.proj-card__svc-input {
  width: 92px; padding: 3px 6px; border: 1px solid var(--border); border-radius: 5px;
  background: var(--bg); color: var(--fg); font-size: 12px; outline: none;
  flex-shrink: 0;
}
.proj-card__svc-input:focus { border-color: var(--accent); }
.proj-card__svc-input--err { border-color: #f44336 !important; background: rgba(244,67,54,.06); }
.proj-card__svc-arrow { color: var(--muted-2); font-size: 12px; margin: 0 2px; flex-shrink: 0; }
/* 建议端口只读预览：黑色不可编辑 */
.proj-card__svc-suggest {
  font-size: 11px; color: var(--fg); font-weight: 600;
  background: var(--bg); border: 1px dashed var(--border); border-radius: 4px;
  padding: 2px 6px; flex-shrink: 0; user-select: none;
}
.svc-starting {
  margin-top: 10px; padding: 10px 12px; border: 1px solid var(--border);
  border-radius: var(--radius); background: var(--bg-2);
}
.svc-starting__row { display: flex; align-items: center; gap: 8px; padding: 3px 0; font-size: 12px; }
.svc-starting__port { color: var(--muted-2); }
.svc-starting__stage { font-size: 11px; padding: 1px 8px; border-radius: 99px; }
.svc-starting__detail { font-size: 11px; color: var(--accent); cursor: pointer; margin-left: auto; flex-shrink: 0; }
.svc-starting__log {
  margin: 4px 0 8px; padding: 8px 10px; font-size: 11px; line-height: 1.5;
  background: var(--bg); border: 1px solid var(--border); border-radius: 6px;
  color: var(--fg-2); font-family: var(--mono); white-space: pre-wrap; word-break: break-all;
  max-height: 200px; overflow-y: auto; user-select: text; cursor: text;
}
.svc-starting__stage--running { background: rgba(76,175,80,.15); color: #4caf50; }
.svc-starting__stage--failed { background: rgba(244,67,54,.16); color: #f44336; }
.svc-starting__stage--starting { background: rgba(33,150,243,.15); color: #2196f3; }
.proj-card {
  /* grid 两列：name/desc 占 1fr、meta 固定 auto；chips 用 grid-column:1/-1 独占第二行。
     之前用 flex 时 chips 的 grid-column 不生效，与 name/meta 挤同一行，空间不足时
     模块 tag 被压缩换行竖排（用户反馈"tag 竖起来了"）。 */
  display: grid; grid-template-columns: 1fr auto; align-items: center;
  gap: 6px 10px;
  padding: 12px 14px; margin-bottom: 8px; border: 1px solid var(--border);
  border-radius: var(--radius); background: var(--card); cursor: pointer;
  transition: border-color .15s, background .15s;
}
.proj-card:hover { border-color: var(--accent); background: var(--card-hover); }
.proj-card--active { border-color: var(--accent); }
.proj-card--working { opacity: .6; pointer-events: none; }
.proj-card__name { font-size: 14px; font-weight: 600; display: flex; align-items: center; gap: 8px; }
.proj-card__cur { font-size: 10px; padding: 1px 6px; border-radius: 99px; background: var(--accent-soft); color: var(--accent); }
.proj-card__desc { font-size: 12px; color: var(--muted-2); margin-top: 2px; }
.proj-card__meta { display: flex; flex-direction: column; align-items: flex-end; gap: 4px; font-size: 11px; color: var(--muted-2); flex-shrink: 0; }
.proj-card__git { max-width: 220px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-family: var(--mono); }

.cred-box { margin-top: 12px; padding: 14px; border: 1px solid var(--accent); border-radius: var(--radius); background: var(--bg-2); }
.cred-box__title { font-size: 13px; font-weight: 600; margin-bottom: 10px; }
.cred-box__mode { display: flex; gap: 6px; margin-bottom: 10px; }
.cred-mode-btn { padding: 4px 12px; font-size: 12px; border: 1px solid var(--border); border-radius: 99px; background: var(--bg); color: var(--muted); cursor: pointer; }
.cred-mode-btn.active { background: var(--accent); color: #000; border-color: var(--accent); font-weight: 600; }
.cred-box__row { margin-bottom: 8px; }
.cred-box__row label { display: block; font-size: 11px; color: var(--muted-2); margin-bottom: 3px; }
.cred-box__row input { width: 100%; padding: 6px 8px; border: 1px solid var(--border); border-radius: 4px; background: var(--bg); color: var(--fg); font-size: 12px; }
.cred-box__err { color: var(--danger); font-size: 12px; margin: 6px 0; }
.cred-box__actions { display: flex; gap: 8px; justify-content: flex-end; margin-top: 10px; }
.proj-card__expand { margin-top: 4px; font-size: 11px; padding: 2px 10px; border: 1px solid var(--border); border-radius: 99px; background: var(--bg-2); color: var(--muted); cursor: pointer; }
.proj-card__expand:hover { border-color: var(--accent); color: var(--accent); }
.proj-card__sel-n { margin-left: 3px; font-weight: 700; color: var(--accent); }
.proj-card__chips { grid-column: 1 / -1; display: flex; gap: 4px; flex-wrap: wrap; margin-top: 2px; }
.proj-card__chip { font-size: 10px; padding: 1px 8px; border-radius: 99px; background: var(--accent-soft); color: var(--accent); max-width: 140px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.proj-card__chip--more { background: var(--bg-2); color: var(--muted-2); }
.proj-card__svc { display: flex; align-items: center; gap: 8px; padding: 6px 10px; border-radius: 6px; cursor: pointer; font-size: 12px; border: 1px solid transparent; margin-bottom: 4px; flex-wrap: nowrap; }
.proj-card__svc:hover { background: var(--card-hover); }
.proj-card__svc--sel { border-color: var(--accent); background: var(--accent-soft); }
.proj-card__svc-check { width: 14px; height: 14px; border-radius: 3px; border: 1px solid var(--border); background: var(--bg); display: inline-flex; align-items: center; justify-content: center; font-size: 10px; color: #000; flex-shrink: 0; }
.proj-card__svc--sel .proj-card__svc-check { background: var(--accent); border-color: var(--accent); }
.proj-card__svc-name { font-weight: 600; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.proj-card__svc-type { font-size: 10px; padding: 0 6px; border-radius: 99px; background: var(--bg-2); color: var(--muted-2); flex-shrink: 0; }
.proj-card__svc-port { font-size: 11px; color: var(--muted-2); font-family: var(--mono); flex-shrink: 0; }
</style>
