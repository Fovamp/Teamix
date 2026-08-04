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

// 模块选择（假选择，为资源池预留）：按项目多选，点项目卡片才真正选择项目。
// 缓存按用户隔离，避免 admin 勾选的模块泄漏到其他用户。
const selectedByProject = ref<Record<string, string[]>>({})
const svcStoreKey = () => "teamix_selected_services_" + (api.currentUser() || "default")
const showModule = ref(false)
const moduleProject = ref("")
const moduleServices = ref<any[]>([])
const moduleLoading = ref(false)
const moduleSel = ref<string[]>([])

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
      selectedByProject.value = JSON.parse(localStorage.getItem(svcStoreKey()) || "{}")
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
  if (working.value || cloneActive.value) {
    toast("项目拉取进行中，请等待完成", "info")
    return
  }
  emit("close")
}

// 模块选择（独立二级模态窗，多选假选择，为资源池预留）
async function openModuleModal(project: string) {
  moduleProject.value = project
  moduleSel.value = [...(selectedByProject.value[project] || [])]
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
  if (i >= 0) moduleSel.value.splice(i, 1)
  else moduleSel.value.push(name)
}

function closeModule() {
  if (moduleLoading.value) return
  showModule.value = false
}

// 确定：保存该项目的多选模块到 localStorage（假选择，点项目卡片才真正选项目）
function confirmModule() {
  selectedByProject.value[moduleProject.value] = [...moduleSel.value]
  localStorage.setItem(svcStoreKey(), JSON.stringify(selectedByProject.value))
  showModule.value = false
}

function projectSelected(name: string) {
  return selectedByProject.value[name] || []
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
            <span v-if="s.port" class="proj-card__svc-port">:{{ s.port }}</span>
          </div>
          <div style="margin-top:12px;display:flex;gap:8px;justify-content:center">
            <button class="btn" @click="closeModule">跳过（选整个项目）</button>
            <button class="btn primary" :disabled="moduleSel.length === 0" @click="confirmModule">
              确定选择（{{ moduleSel.length }}）
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
.proj-card {
  display: flex; align-items: center; justify-content: space-between; gap: 10px;
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
.proj-card__svc { display: flex; align-items: center; gap: 8px; padding: 6px 10px; border-radius: 6px; cursor: pointer; font-size: 12px; border: 1px solid transparent; margin-bottom: 4px; }
.proj-card__svc:hover { background: var(--card-hover); }
.proj-card__svc--sel { border-color: var(--accent); background: var(--accent-soft); }
.proj-card__svc-check { width: 14px; height: 14px; border-radius: 3px; border: 1px solid var(--border); background: var(--bg); display: inline-flex; align-items: center; justify-content: center; font-size: 10px; color: #000; flex-shrink: 0; }
.proj-card__svc--sel .proj-card__svc-check { background: var(--accent); border-color: var(--accent); }
.proj-card__svc-name { font-weight: 600; }
.proj-card__svc-type { font-size: 10px; padding: 0 6px; border-radius: 99px; background: var(--bg-2); color: var(--muted-2); }
.proj-card__svc-port { font-size: 11px; color: var(--muted-2); font-family: var(--mono); }
</style>
