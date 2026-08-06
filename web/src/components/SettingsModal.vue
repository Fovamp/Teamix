<script setup lang="ts">
import { ref, watch, onMounted } from "vue"
import { api } from "../api"
import { useToast } from "../composables/useToast"
const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{ (e: "close"): void }>()
const { toast } = useToast()
const isArch = ref(false)
const tab = ref("keys")
const allTabs = ["users", "projects", "keys", "mcp", "soul", "skills", "memory", "sensitive", "audit"]
// 普通用户可见：MCP/Skills/记忆/人格（人格=全局只读可选 + 私有可编辑）；用户/项目/密钥池/安全为架构师专属
const visibleTabs = ref<string[]>(allTabs)
const tabLbl: Record<string, string> = { users: "\u7528\u6237", projects: "\u9879\u76ee", keys: "\u5bc6\u94a5\u6c60", mcp: "MCP", soul: "AI \u4eba\u683c", skills: "Skills", memory: "\u8bb0\u5fc6", sensitive: "\u5b89\u5168", audit: "AI \u5ba1\u8ba1" }
const tabIcon: Record<string, string> = { users: "\ud83d\udc65", projects: "\ud83d\udce6", keys: "\ud83d\udd11", mcp: "\ud83d\udd27", soul: "\ud83e\udde0", skills: "\ud83d\udcdc", memory: "\ud83e\udde0", sensitive: "\ud83d\udee1\ufe0f", audit: "\ud83d\udee1\ufe0f" }

onMounted(async () => {
  try {
    const r = await api.userRole()
    isArch.value = r.role === "architect"
  } catch {}
  visibleTabs.value = isArch.value ? allTabs : ["mcp", "soul", "skills", "memory"]
  // 打开设置默认显示第一个可见页面（架构师=用户，普通用户=MCP）
  tab.value = visibleTabs.value[0] || "keys"
})

// Content state
const contentHtml = ref("\u52a0\u8f7d\u4e2d...")
const loading = ref(false)

watch(() => props.visible, (v) => {
  if (v) { switchTab(tab.value) }
})

watch(tab, (t) => { switchTab(t) })

async function switchTab(t: string) {
  loading.value = true
  contentHtml.value = "\u52a0\u8f7d\u4e2d..."
  try {
    if (t === "users") { await renderUsers() }
    else if (t === "projects") { await renderProjects() }
    else if (t === "keys") { await renderKeys() }
    else if (t === "mcp") { await renderMCP() }
    else if (t === "skills") { await renderSkills() }
    else if (t === "memory") { await renderMemory() }
    else if (t === "sensitive") { await renderSensitive() }
    else if (t === "audit") { await renderAudit() }
    else { await renderSoul() }
  } catch (e: any) {
    contentHtml.value = `<div style="color:#f44336;padding:12px">\u52a0\u8f7d\u5931\u8d25: ${e.message}</div>`
  }
  loading.value = false
}

async function renderUsers() {
  const q = tokenQuery()
  let h = '<div class="section"><h3>\ud83d\udc65 \u7528\u6237\u7ba1\u7406</h3><p class="desc">\u767d\u540d\u5355\u4e0e\u89d2\u8272\u7ba1\u7406\uff08\u4ec5\u67b6\u6784\u5e08\uff09\u3002\u5220\u9664/\u964d\u7ea7\u6700\u540e\u4e00\u4e2a\u67b6\u6784\u5e08\u5c06\u88ab\u62d2\u7edd\u3002</p></div><div id="users-render">'
  try {
    const resp = await fetch("/teamix/users" + q)
    const data = await resp.json()
    const users = data.users || []
    h += '<div class="section"><div class="section-title">\u7528\u6237\u5217\u8868 (' + users.length + ')</div>'
    if (users.length === 0) h += '<div style="color:var(--muted-2);text-align:center;padding:16px;font-size:13px">\u6682\u65e0\u7528\u6237</div>'
    users.forEach((u: any) => {
      const cur = u.isCurrent ? ' <span style="font-size:10px;padding:1px 6px;border-radius:99px;background:var(--accent-soft);color:var(--accent)">\u5f53\u524d</span>' : ''
      const credBadge = u.credentialConfigured
        ? ' <span style="font-size:10px;padding:1px 6px;border-radius:99px;background:rgba(52,199,89,.15);color:#34c759">\u5df2\u914d\u51ed\u8bc1</span>'
        : ' <span style="font-size:10px;padding:1px 6px;border-radius:99px;background:var(--bg-2);color:var(--muted-2)">\u672a\u914d\u51ed\u8bc1</span>'
      h += '<div class="card" style="flex-direction:row;align-items:center;justify-content:space-between;padding:8px 12px">'
      h += '<div class="card-info"><div class="card-title"><code>' + escH(u.name) + '</code>' + cur + credBadge + '</div></div>'
      h += '<div style="display:flex;gap:6px;align-items:center">'
      h += '<select data-user-role="' + escAttr(u.name) + '" style="padding:4px 6px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:12px"><option value="developer"' + (u.role === 'developer' ? ' selected' : '') + '>developer</option><option value="architect"' + (u.role === 'architect' ? ' selected' : '') + '>architect</option></select>'
      h += '<button class="btn sm" data-user-edit="' + escAttr(u.name) + '" id="user-editbtn-' + escAttr(u.name) + '" style="padding:4px 10px;border:1px solid var(--border);border-radius:4px;background:var(--bg-2);color:var(--fg);font-size:11px;cursor:pointer">\u7f16\u8f91</button>'
      h += '<button class="btn danger sm" data-user-del="' + escAttr(u.name) + '" style="padding:4px 10px;border:1px solid var(--danger);border-radius:4px;background:var(--danger-soft);color:var(--danger);font-size:11px;cursor:pointer">\u5220\u9664</button>'
      h += '</div></div>'
      h += '<div id="user-edit-' + escAttr(u.name) + '" style="display:none;padding:8px 12px;border-bottom:1px solid var(--border)"><div style="font-size:11px;color:var(--muted-2);margin-bottom:4px">Git HTTPS \u51ed\u8bc1\uff08\u8d26\u53f7 / \u8bbf\u95ee\u4ee4\u724c\uff09</div><div style="display:flex;gap:8px;margin-bottom:6px"><input id="edit-uuser-' + escAttr(u.name) + '" type="text" placeholder="\u8d26\u53f7 / oauth2" style="flex:1;padding:5px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:12px"><input id="edit-upass-' + escAttr(u.name) + '" type="password" placeholder="\u5bc6\u7801 / \u4ee4\u724c" style="flex:1;padding:5px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:12px"></div><button class="btn primary sm" data-user-save-cred="' + escAttr(u.name) + '" style="padding:4px 14px;border:none;border-radius:4px;background:var(--accent);color:#000;font-size:12px;cursor:pointer">\u4fdd\u5b58\u51ed\u8bc1</button></div>'
    })
    h += '<div class="section"><div class="section-title">\u6dfb\u52a0\u7528\u6237</div>'
    h += '<div style="display:flex;gap:8px;align-items:flex-end;margin-bottom:8px">'
    h += '<div style="flex:1"><label style="font-size:11px;color:var(--muted-2);display:block;margin-bottom:2px">\u6635\u79f0</label><input id="user-name" type="text" placeholder="\u5982 alice" style="width:100%;padding:5px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:12px"></div>'
    h += '<div style="flex:1"><label style="font-size:11px;color:var(--muted-2);display:block;margin-bottom:2px">\u89d2\u8272</label><select id="user-role" style="width:100%;padding:5px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:12px"><option value="developer">developer</option><option value="architect">architect</option></select></div>'
    h += '<div style="text-align:right"><button class="btn primary" onclick="addUser()" style="padding:6px 16px;border:none;border-radius:4px;background:var(--accent);color:#000;font-size:12px;cursor:pointer">\u6dfb\u52a0</button></div></div>'
    h += '<div style="font-size:11px;color:var(--muted-2);margin-bottom:4px">Git HTTPS \u51ed\u8bc1\uff08\u53ef\u9009\uff09\uff1a\u586b\u5199\u540e\u8be5\u7528\u6237\u767b\u5f55\u5373\u53ef\u76f4\u63a5\u62c9\u53d6\u4ee3\u7801</div>'
    h += '<div style="display:flex;gap:8px;margin-bottom:8px">'
    h += '<div style="flex:1"><label style="font-size:11px;color:var(--muted-2);display:block;margin-bottom:2px">\u8d26\u53f7 / \u4ee4\u724c\u7528\u6237\u540d</label><input id="user-https-user" type="text" placeholder="\u5e73\u53f0\u8d26\u53f7 \u6216 oauth2" style="width:100%;padding:5px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:12px"></div>'
    h += '<div style="flex:1"><label style="font-size:11px;color:var(--muted-2);display:block;margin-bottom:2px">\u5bc6\u7801 / \u8bbf\u95ee\u4ee4\u724c</label><input id="user-https-pass" type="password" placeholder="\u5bc6\u7801\u6216\u9879\u76ee\u8bbf\u95ee\u4ee4\u724c" style="width:100%;padding:5px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:12px"></div>'
    h += '</div></div>'
  } catch (e) {
    h += '<div style="color:#f44336;padding:12px">\u52a0\u8f7d\u5931\u8d25</div>'
  }
  h += '</div>'
  contentHtml.value = h
}

async function renderProjects() {
  const q = tokenQuery()
  let h = '<div class="section"><h3>\ud83d\udce6 \u9879\u76ee\u7ba1\u7406</h3><p class="desc">\u9879\u76ee\u6e05\u5355\u7ba1\u7406\uff08\u4ec5\u67b6\u6784\u5e08\uff09\u3002\u6dfb\u52a0\u65f6\u4f1a\u6821\u9a8c git \u94fe\u63a5\u53ef\u8bbf\u95ee\uff0c\u5f00\u53d1\u8005\u9009\u62e9\u9879\u76ee\u65f6\u5404\u81ea clone\u3002</p></div><div id="projects-render">'
  try {
    const resp = await fetch("/teamix/projects" + q)
    const projects = await resp.json()
    h += '<div class="section"><div class="section-title">\u9879\u76ee\u5217\u8868 (' + projects.length + ')</div>'
    if (projects.length === 0) h += '<div style="color:var(--muted-2);text-align:center;padding:16px;font-size:13px">\u6682\u65e0\u9879\u76ee</div>'
    projects.forEach((p: any) => {
      h += '<div class="card" style="flex-direction:row;align-items:center;justify-content:space-between;padding:8px 12px">'
      h += '<div class="card-info"><div class="card-title">' + escH(p.name) + ' <span style="font-size:10px;color:var(--muted-2)">' + (p.serviceCount || 0) + ' \u4e2a\u670d\u52a1</span></div><div class="card-sub" style="font-size:11px;color:var(--muted-2)">' + escH(p.git) + (p.description ? ' \u00b7 ' + escH(p.description) : '') + '</div></div>'
      h += '<div style="display:flex;gap:6px;align-items:center"><button class="btn sm" data-proj-expand="' + escAttr(p.name) + '" id="proj-exp-' + escAttr(p.name) + '" style="padding:4px 10px;border:1px solid var(--border);border-radius:4px;background:var(--bg-2);color:var(--fg);font-size:11px;cursor:pointer">\u5c55\u5f00</button>'
      h += '<button class="btn sm" data-proj-edit="' + escAttr(p.name) + '" id="proj-editbtn-' + escAttr(p.name) + '" style="padding:4px 10px;border:1px solid var(--border);border-radius:4px;background:var(--bg-2);color:var(--fg);font-size:11px;cursor:pointer">\u7f16\u8f91</button>'
      h += '<button class="btn sm" data-proj-scan="' + escAttr(p.name) + '" style="padding:4px 10px;border:1px solid var(--border);border-radius:4px;background:var(--bg-2);color:var(--fg);font-size:11px;cursor:pointer">\u91cd\u65b0\u626b\u63cf</button>'
      h += '<button class="btn danger sm" data-proj-del="' + escAttr(p.name) + '" style="padding:4px 10px;border:1px solid var(--danger);border-radius:4px;background:var(--danger-soft);color:var(--danger);font-size:11px;cursor:pointer">\u5220\u9664</button></div>'
      h += '</div>'
      h += '<div class="cfg-svc-list" id="proj-svc-' + escAttr(p.name) + '" style="display:none"></div>'
      h += '<div id="proj-edit-' + escAttr(p.name) + '" style="display:none;padding:8px 12px;border-bottom:1px solid var(--border)"><div style="display:flex;gap:8px;margin-bottom:6px"><input id="edit-git-' + escAttr(p.name) + '" type="text" value="' + escAttr(p.git || "") + '" placeholder="git \u94fe\u63a5 (SSH/HTTPS)" style="flex:2;padding:5px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:12px"><input id="edit-desc-' + escAttr(p.name) + '" type="text" value="' + escAttr(p.description || "") + '" placeholder="\u63cf\u8ff0" style="flex:1;padding:5px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:12px"></div><button class="btn primary sm" data-proj-save="' + escAttr(p.name) + '" style="padding:4px 14px;border:none;border-radius:4px;background:var(--accent);color:#000;font-size:12px;cursor:pointer">\u4fdd\u5b58</button></div>'
      h += '<div class="cfg-progress" id="proj-bar-' + escAttr(p.name) + '" style="display:none"><div class="cfg-progress__bar"></div><span>\u6b63\u5728\u62c9\u53d6\u4ee3\u7801\u5e76\u626b\u63cf\u6a21\u5757...</span></div>'
    })
    h += '<div class="section"><div class="section-title">\u6dfb\u52a0\u9879\u76ee</div>'
    h += '<div id="proj-err" style="color:var(--danger);font-size:12px;margin-bottom:6px"></div>'
    h += '<div style="display:flex;gap:8px;margin-bottom:8px"><div style="flex:1"><label style="font-size:11px;color:var(--muted-2);display:block;margin-bottom:2px">\u540d\u79f0</label><input id="proj-name" type="text" placeholder="mall-system" style="width:100%;padding:5px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:12px"></div><div style="flex:2"><label style="font-size:11px;color:var(--muted-2);display:block;margin-bottom:2px">Git \u94fe\u63a5</label><input id="proj-git" type="text" placeholder="git@github.com:team/mall-system.git" style="width:100%;padding:5px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:12px"></div></div>'
    h += '<div style="margin-bottom:8px"><label style="font-size:11px;color:var(--muted-2);display:block;margin-bottom:2px">\u63cf\u8ff0</label><input id="proj-desc" type="text" placeholder="\u7535\u5546\u7cfb\u7edf" style="width:100%;padding:5px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:12px"></div>'
    h += '<div class="cfg-progress" id="proj-add-bar" style="display:none"><div class="cfg-progress__bar"></div><span>\u6b63\u5728\u9a8c\u8bc1 git \u94fe\u63a5\u5e76\u62c9\u53d6\u4ee3\u7801...</span></div>'
    h += '<div style="text-align:right"><button class="btn primary" onclick="addProject()" style="padding:6px 16px;border:none;border-radius:4px;background:var(--accent);color:#000;font-size:12px;cursor:pointer">\u6dfb\u52a0\u9879\u76ee</button></div></div>'
  } catch (e) {
    h += '<div style="color:#f44336;padding:12px">\u52a0\u8f7d\u5931\u8d25</div>'
  }
  h += '</div>'
  contentHtml.value = h
}

async function renderKeys() {
  const q = tokenQuery()
  const resp = await fetch("/teamix/secrets/status" + q)
  const data = await resp.json()
  const keys = data.keyList || []
  let h = '<div class="section"><h3>\ud83d\udd11 \u5bc6\u94a5\u6c60</h3><p class="desc">\u56e2\u961f\u5171\u4eab\u7684 API Key\uff0c\u8d1f\u8f7d\u5747\u8861\u5206\u53d1\u5230\u6bcf\u4e2a Agent \u4f1a\u8bdd\u3002\u5bc6\u94a5\u4ec5\u5b58\u50a8\u5728\u670d\u52a1\u5668\u672c\u5730 .reasonix/secrets/ \u76ee\u5f55\u3002</p></div><div id="key-render">'
  h += '<div class="section"><div class="section-title">\u8d1f\u8f7d\u7b56\u7565</div>'
  h += '<div class="card" style="flex-direction:row;align-items:center;justify-content:space-between;padding:8px 12px"><div class="card-info"><div class="card-title">\u5206\u914d\u65b9\u5f0f</div><div class="card-sub">\u73af\u5883\u53d8\u91cf: <code>' + (data.target || '-') + '</code></div></div>'
  h += '<select id="key-strategy-select" style="width:140px;padding:5px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:12px;cursor:pointer">'
  h += '<option value="round-robin"' + (data.strategy === "round-robin" ? " selected" : "") + '>Round Robin</option>'
  h += '<option value="random"' + (data.strategy === "random" ? " selected" : "") + '>Random</option>'
  h += '</select>'
  h += '<div style="text-align:right"><button class="btn" onclick="saveKeyStrategy()" style="padding:5px 12px;border:1px solid var(--border);border-radius:4px;background:var(--bg-2);color:var(--fg);font-size:12px;cursor:pointer">\u5e94\u7528</button></div></div></div>'
  h += '<div class="section"><div class="section-title">\u5bc6\u94a5\u5217\u8868 (' + keys.length + ')</div>'
  if (keys.length === 0) h += '<div style="color:var(--muted-2);text-align:center;padding:16px 0;font-size:13px">\u5c1a\u65e0\u5bc6\u94a5</div>'
  keys.forEach((k: any) => {
    h += '<div class="card" style="flex-direction:row;align-items:center;justify-content:space-between;padding:8px 12px">'
    h += '<div class="card-info"><div class="card-title"><code>' + k.envName + '</code></div><div class="card-sub">\u4f7f\u7528 ' + k.useCount + ' \u6b21</div></div>'
    h += '<span class="badge ' + (k.enabled ? "on" : "off") + '" style="padding:1px 8px;border-radius:99px;font-size:10px;font-weight:500;">' + (k.enabled ? "\u5df2\u542f\u7528" : "\u5df2\u7981\u7528") + '</span>'
    h += '<button class="btn danger sm" data-key-del="' + escAttr(k.envName) + '" style="padding:3px 10px;border:1px solid var(--danger);border-radius:4px;background:var(--danger-soft);color:var(--danger);font-size:11px;cursor:pointer">\u5220\u9664</button></div>'
  })
  h += '<div class="section"><div class="section-title">\u6dfb\u52a0\u5bc6\u94a5</div>'
  h += '<div style="display:flex;gap:8px;margin-bottom:8px"><input id="new-key-env" type="text" placeholder="\u73af\u5883\u53d8\u91cf\u540d" style="flex:1;padding:6px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:12px"><input id="new-key-value" type="password" placeholder="API Key" style="flex:2;padding:6px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:12px"></div>'
  h += '<div style="text-align:right"><button class="btn primary" onclick="addKey()" style="padding:6px 16px;border:none;border-radius:4px;background:var(--accent);color:#000;font-size:12px;cursor:pointer">\u6dfb\u52a0\u5bc6\u94a5</button></div></div>'
  h += '</div>'
  contentHtml.value = h
}

// 敏感级徽章 HTML（数据源声明分级展示）：空 = 未声明
function sensBadge(s: string): string {
  if (!s) return ''
  const colors: Record<string, string> = {
    public: 'background:rgba(76,175,80,.15);color:#4caf50',
    internal: 'background:rgba(255,152,0,.15);color:#ff9800',
    redact: 'background:rgba(33,150,243,.15);color:#2196f3',
    confidential: 'background:rgba(244,67,54,.15);color:#f44336'
  }
  const c = colors[s] || colors.internal
  return '<span style="margin-left:6px;font-size:10px;padding:1px 6px;border-radius:99px;' + c + '">' + escH(s) + '</span>'
}
// 敏感级下拉 HTML（数据源声明分级表单）：3 档出网策略，默认 internal（禁止出网）。
// 原 confidential 已并入 internal（行为相同，仅保留兼容）。
function sensSelect(id: string): string {
  return '<div class="input-row" style="margin-bottom:8px"><label style="font-size:11px;color:var(--muted-2);display:block;margin-bottom:2px">\u6570\u636e\u654f\u611f\u7ea7</label><select id="' + id + '" style="width:100%;padding:5px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:12px"><option value="internal" selected>internal\uff08\u7981\u6b62\u51fa\u7f51\uff0c\u9ed8\u8ba4\uff09</option><option value="public">public\uff08\u53ef\u51fa\u7f51\uff09</option><option value="redact">redact\uff08\u5047\u540d\u5316\u540e\u53ef\u51fa\u7f51\uff09</option></select></div>'
}

async function renderMCP() {
  const q = tokenQuery()
  contentHtml.value = '<div class="section"><h3>🔧 MCP 服务器</h3><p class="desc">管理 MCP 服务器，扩展 Agent 的工具能力。</p><div style="color:var(--muted-2);text-align:center;padding:16px;font-size:13px">加载中...</div>'
  let h = '<div class="section"><h3>\ud83d\udd27 MCP \u670d\u52a1\u5668</h3><p class="desc">\u7ba1\u7406 MCP \u670d\u52a1\u5668\uff0c\u6269\u5c55 Agent \u7684\u5de5\u5177\u80fd\u529b\u3002</p></div><div id="mcp-render">'
  try {
    let servers: any[] = []
    try {
      const resp = await fetch("/teamix/mcp/servers" + q)
      servers = await resp.json()
    } catch (e) { }
    let role = ""
    try {
      const rr = await fetch("/teamix/user/role" + q)
      role = ((await rr.json()).role || "") as string
    } catch (e) { }
    const isArch = role === "architect"
    if (servers.length === 0) {
      h += '<div style="color:var(--muted-2);text-align:center;padding:20px;font-size:13px">\u5c1a\u65e0 MCP \u670d\u52a1\u5668</div>'
    }
    servers.forEach((s: any) => {
      const toolHtml = s.toolList && s.toolList.length > 0
        ? '<div style="padding:4px 0;font-size:12px;font-weight:500;color:var(--muted-2)">\u5de5\u5177\u5217\u8868 (' + s.tools + '):</div>' +
          s.toolList.map((t: any) => '<div style="padding:3px 4px;border-bottom:1px solid var(--border)"><code>' + t.name + '</code>' +
            (t.description ? '<br><span style="color:var(--muted-2);font-size:11px">' + escH(t.description) + '</span>' : '') + '</div>').join('')
        : ''
      const isFailed = s.status === "failed"
      const srcBadge = s.source === "global"
        ? '<span style="margin-left:6px;font-size:10px;padding:1px 6px;border-radius:99px;background:rgba(76,175,80,.15);color:#4caf50">\u5168\u5c40</span>'
        : '<span style="margin-left:6px;font-size:10px;padding:1px 6px;border-radius:99px;background:rgba(150,150,150,.15);color:var(--muted-2)">\u79c1\u6709</span>'
      h += '<div class="card" style="flex-direction:column;align-items:stretch" data-open="false">'
      h += '<div class="card-head" onclick="const c=this.closest(\'.card\');const b=c.querySelector(\'.card-body\');const o=c.dataset.open===\'true\';c.dataset.open=o?\'false\':\'true\';b.style.display=o?\'none\':\'\'">'
      h += '<span class="chev" style="color:var(--muted-2);transition:transform .15s;display:inline-flex"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="6 9 12 15 18 9"/></svg></span>'
      h += '<div class="card-main"><div class="card-title"><span class="name"><code>' + escH(s.name) + '</code></span>' + srcBadge + sensBadge(s.sensitivity) + '</div>'
      h += '<span class="subject">' + (s.transport || "stdio") + ' \u00b7 ' + s.tools + ' \u4e2a\u5de5\u5177' + (isFailed ? ' <span style="color:#f44336">\u79bb\u7ebf</span>' : '') + '</span></div>'
      h += '</div>'
      h += '<div class="card-body" style="display:none;padding:8px 12px;border-top:1px solid var(--border);background:var(--bg-2);font-size:12px;color:var(--fg-2)">' + (isFailed && s.error ? '<span style="color:#f44336;font-size:11px">' + escH(s.error) + '</span>' : (toolHtml || '<span style="color:var(--muted-2)">\u65e0\u5de5\u5177</span>')) + '<div style="margin-top:8px;text-align:right"><button class="btn danger sm" data-mcp-remove="' + escAttr(s.name) + '" style="padding:3px 10px;border:1px solid var(--danger);border-radius:4px;background:var(--danger-soft);color:var(--danger);font-size:11px;cursor:pointer">\u79fb\u9664</button></div></div>'
      h += '</div>'
    })
    h += '<div class="section"><div class="section-title">\u6dfb\u52a0 MCP \u670d\u52a1\u5668</div>'
    h += '<div class="input-row" style="margin-bottom:8px"><label style="font-size:11px;color:var(--muted-2);display:block;margin-bottom:2px">\u540d\u79f0</label><input id="mcp-name" type="text" placeholder="server-name" style="width:100%;padding:5px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:12px"></div>'
    h += '<div class="input-row" style="margin-bottom:8px"><label style="font-size:11px;color:var(--muted-2);display:block;margin-bottom:2px">\u547d\u4ee4</label><input id="mcp-cmd" type="text" placeholder="npx" style="width:100%;padding:5px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:12px"></div>'
    h += '<div class="input-row" style="margin-bottom:8px"><label style="font-size:11px;color:var(--muted-2);display:block;margin-bottom:2px">\u4f20\u8f93</label><select id="mcp-transport" style="width:100%;padding:5px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:12px"><option value="stdio">stdio</option><option value="http">http</option></select></div>'
    h += '<div style="margin-bottom:8px"><label style="font-size:11px;color:var(--muted-2);display:block;margin-bottom:2px">\u53c2\u6570</label><input id="mcp-args" type="text" placeholder="-y @modelcontextprotocol/server-filesystem" style="width:100%;padding:5px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:12px"></div>'
    if (isArch) {
      h += '<div class="input-row" style="margin-bottom:8px"><label style="font-size:11px;color:var(--muted-2);display:block;margin-bottom:2px">\u8303\u56f4</label><select id="mcp-scope" style="width:100%;padding:5px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:12px"><option value="private">\u79c1\u6709\uff08\u4ec5\u81ea\u5df1\u53ef\u7528\uff09</option><option value="global">\u5168\u5c40\uff08\u5199\u5165\u516c\u5171\u914d\u7f6e\uff0c\u5168\u5458\u53ef\u7528\uff09</option></select></div>'
    }
    h += sensSelect("mcp-sens")
    h += '<div style="text-align:right"><button class="btn primary" onclick="addMCPServer()" style="padding:6px 16px;border:none;border-radius:4px;background:var(--accent);color:#000;font-size:12px;cursor:pointer">\u6dfb\u52a0\u670d\u52a1\u5668</button></div></div>'
  } catch (e: any) {
    h += '<div style="color:#f44336;padding:12px">\u52a0\u8f7d\u5931\u8d25: ' + e.message + '</div>'
  }
  h += '</div>'
  contentHtml.value = h
}

async function renderSkills() {
  const q = tokenQuery()
  let h = '<div class="section"><h3>\ud83d\udcdc Skills</h3><p class="desc">\u7ba1\u7406 Agent \u53ef\u7528\u7684\u6280\u80fd\u3002</p></div><div id="skills-render">'
  try {
    let role = ""
    try {
      const rr = await fetch("/teamix/user/role" + q)
      role = ((await rr.json()).role || "") as string
    } catch (e) { }
    const isArch = role === "architect"
    const resp = await fetch("/teamix/skills" + q)
    const skills = await resp.json()
    h += '<div class="section"><div class="section-title">\u6280\u80fd\u5217\u8868 (' + skills.length + ')</div>'
    if (skills.length === 0) h += '<div style="color:var(--muted-2);text-align:center;padding:20px;font-size:13px">\u5c1a\u65e0 Skills</div>'
    skills.forEach((s: any) => {
      const hasDesc = s.description && s.description.length > 0
      const isGlobalScope = s.scope === "global" || s.scope === "custom"
      const scopeBadge = isGlobalScope
        ? '<span style="margin-left:6px;font-size:10px;padding:1px 6px;border-radius:99px;background:rgba(76,175,80,.15);color:#4caf50">\u5168\u5c40</span>'
        : '<span style="margin-left:6px;font-size:10px;padding:1px 6px;border-radius:99px;background:rgba(150,150,150,.15);color:var(--muted-2)">' + (s.scope === "builtin" ? "\u5185\u7f6e" : "\u79c1\u6709") + '</span>'
      h += '<div class="card" style="flex-direction:column;align-items:stretch" data-open="false">'
      h += '<div class="card-head" onclick="const c=this.closest(\'.card\');const b=c.querySelector(\'.card-body\');const o=c.dataset.open===\'true\';c.dataset.open=o?\'false\':\'true\';b.style.display=o?\'none\':\'\'">'
      h += '<span class="chev" style="color:var(--muted-2);transition:transform .15s;display:inline-flex"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="6 9 12 15 18 9"/></svg></span>'
      h += '<div class="card-main"><div class="card-title"><span class="name">' + escH(s.name) + '</span>' + scopeBadge + sensBadge(s.sensitivity) + '</div>'
      h += '<span class="subject">' + (s.scope || "project") + '</span></div>'
      h += '<button class="btn danger sm" data-skill-del="' + escAttr(s.name) + '" data-skill-scope="' + (isGlobalScope ? "global" : "private") + '" style="padding:3px 10px;border:1px solid var(--danger);border-radius:4px;background:var(--danger-soft);color:var(--danger);font-size:11px;cursor:pointer">\u5220\u9664</button>'
      h += '</div>'
      h += '<div class="card-body" style="display:none;padding:8px 12px;border-top:1px solid var(--border);background:var(--bg-2);font-size:12px;color:var(--fg-2)">' + (hasDesc ? escH(s.description) : '<span style="color:var(--muted-2)">\u6682\u65e0\u63cf\u8ff0</span>') + '</div>'
      h += '</div>'
    })
    // 添加 Skill
    h += '<div class="section"><div class="section-title">\u6dfb\u52a0 Skill</div>'
    h += '<div class="input-row" style="margin-bottom:8px"><label style="font-size:11px;color:var(--muted-2);display:block;margin-bottom:2px">\u540d\u79f0</label><input id="skill-name" type="text" placeholder="my-skill" style="width:100%;padding:5px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:12px"></div>'
    h += '<div class="input-row" style="margin-bottom:8px"><label style="font-size:11px;color:var(--muted-2);display:block;margin-bottom:2px">\u63cf\u8ff0</label><input id="skill-desc" type="text" placeholder="\u4e00\u884c\u63cf\u8ff0" style="width:100%;padding:5px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:12px"></div>'
    if (isArch) {
      h += '<div class="input-row" style="margin-bottom:8px"><label style="font-size:11px;color:var(--muted-2);display:block;margin-bottom:2px">\u8303\u56f4</label><select id="skill-scope" style="width:100%;padding:5px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:12px"><option value="private">\u79c1\u6709\uff08\u4ec5\u81ea\u5df1\u53ef\u7528\uff09</option><option value="global">\u5168\u5c40\uff08\u5168\u5458\u7ee7\u627f\uff09</option></select></div>'
    }
    h += sensSelect("skill-sens")
    h += '<div style="margin-bottom:8px"><label style="font-size:11px;color:var(--muted-2);display:block;margin-bottom:2px">\u5185\u5bb9</label><textarea id="skill-body" style="min-height:100px;width:100%;padding:6px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:12px;font-family:var(--mono)" placeholder="\u64cd\u4f5c\u6307\u5357 markdown..."></textarea></div>'
    h += '<div style="text-align:right"><button class="btn primary" onclick="addSkill()" style="padding:6px 16px;border:none;border-radius:4px;background:var(--accent);color:#000;font-size:12px;cursor:pointer">\u4fdd\u5b58 Skill</button></div></div>'
  } catch (e) {
    h += '<div style="color:#f44336;padding:12px">\u52a0\u8f7d\u5931\u8d25</div>'
  }
  h += '</div>'
  contentHtml.value = h
}

async function renderMemory() {
  const q = tokenQuery()
  let h = '<div class="section"><h3>\ud83e\udde0 \u8bb0\u5fc6</h3><p class="desc">\u67e5\u770b\u548c\u7ba1\u7406 Agent \u8bb0\u4f4f\u7684\u4e8b\u5b9e\u3002</p></div><div id="mem-render">'
  try {
    let role = ""
    try {
      const rr = await fetch("/teamix/user/role" + q)
      role = ((await rr.json()).role || "") as string
    } catch (e) { }
    const isArch = role === "architect"

    // 全局记忆（架构师维护，全员只读继承）
    let globalMem: any[] = []
    try {
      const t = localStorage.getItem("teamix_token")
      const gUrl = "/teamix/memory?scope=global" + (t ? "&token=" + encodeURIComponent(t) : "")
      const g = await (await fetch(gUrl)).json()
      globalMem = g.memories || []
    } catch (e) { }
    h += '<div class="section"><div class="section-title">\u5168\u5c40\u8bb0\u5fc6 (' + globalMem.length + ') <span style="font-size:11px;color:var(--muted-2)">\u56e2\u961f\u5171\u4eab\uff0c\u67b6\u6784\u5e08\u7ef4\u62a4</span></div>'
    if (globalMem.length === 0) h += '<div style="color:var(--muted-2);text-align:center;padding:12px;font-size:13px">\u5c1a\u65e0\u5168\u5c40\u8bb0\u5fc6</div>'
    globalMem.forEach((m: any) => {
      const bodyPreview = m.body ? m.body.slice(0, 80).replace(/</g, "&lt;") : ""
      const hasMore = m.body && m.body.length > 80
      h += '<div class="card" style="flex-direction:column;align-items:stretch;border-color:rgba(76,175,80,.4)" data-open="false">'
      h += '<div class="card-head" onclick="const c=this.closest(\'.card\');const b=c.querySelector(\'.card-body\');const o=c.dataset.open===\'true\';c.dataset.open=o?\'false\':\'true\';b.style.display=o?\'none\':\'\'">'
      h += '<span class="chev" style="color:var(--muted-2);transition:transform .15s;display:inline-flex"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="6 9 12 15 18 9"/></svg></span>'
      h += '<div class="card-main"><div class="card-title"><span class="name">' + escH(m.title || m.name) + '</span>'
      h += '<span style="margin-left:6px;font-size:10px;padding:1px 6px;border-radius:99px;background:rgba(76,175,80,.15);color:#4caf50">\u5168\u5c40</span>' + sensBadge(m.sensitivity) + '</div>'
      if (m.description) h += '<span class="subject">' + escH(m.description) + '</span>'
      else if (bodyPreview) h += '<span class="subject">' + bodyPreview + (hasMore ? "..." : "") + '</span>'
      h += '</div>'
      if (isArch) h += '<button class="btn danger sm" data-mem-del-global="' + escAttr(m.name) + '" style="padding:3px 10px;border:1px solid var(--danger);border-radius:4px;background:var(--danger-soft);color:var(--danger);font-size:11px;cursor:pointer">\u5220\u9664</button>'
      h += '</div>'
      h += '<div class="card-body" style="display:none;padding:8px 12px;border-top:1px solid var(--border);background:var(--bg-2);font-size:12px;color:var(--fg-2);white-space:pre-wrap">' + (m.body ? escH(m.body) : '<span style="color:var(--muted-2)">\u65e0\u5185\u5bb9</span>') + '</div>'
      h += '</div>'
    })

    // 私有记忆（本人）
    const resp = await fetch("/teamix/memory" + q)
    const data = await resp.json()
    const memories = data.memories || []
    h += '<div class="section"><div class="section-title">\u6211\u7684\u8bb0\u5fc6 (' + memories.length + ')</div>'
    if (memories.length === 0) h += '<div style="color:var(--muted-2);text-align:center;padding:20px;font-size:13px">\u5c1a\u65e0\u8bb0\u5fc6</div>'
    memories.forEach((m: any) => {
      const typeLabel = m.type || "user"
      const bodyPreview = m.body ? m.body.slice(0, 80).replace(/</g, "&lt;") : ""
      const hasMore = m.body && m.body.length > 80
      h += '<div class="card" style="flex-direction:column;align-items:stretch" data-open="false">'
      h += '<div class="card-head" onclick="const c=this.closest(\'.card\');const b=c.querySelector(\'.card-body\');const o=c.dataset.open===\'true\';c.dataset.open=o?\'false\':\'true\';b.style.display=o?\'none\':\'\'">'
      h += '<span class="chev" style="color:var(--muted-2);transition:transform .15s;display:inline-flex"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="6 9 12 15 18 9"/></svg></span>'
      h += '<div class="card-main"><div class="card-title"><span class="name">' + escH(m.title || m.name) + '</span>'
      h += '<span class="badge" style="margin-left:6px;font-size:10px;padding:1px 6px;border-radius:99px;background:var(--accent-soft);color:var(--accent)">' + typeLabel + '</span>' + sensBadge(m.sensitivity) + '</div>'
      if (m.description) h += '<span class="subject">' + escH(m.description) + '</span>'
      else if (bodyPreview) h += '<span class="subject">' + bodyPreview + (hasMore ? "..." : "") + '</span>'
      h += '</div>'
      h += '<button class="btn danger sm" data-mem-del="' + escAttr(m.name) + '" style="padding:3px 10px;border:1px solid var(--danger);border-radius:4px;background:var(--danger-soft);color:var(--danger);font-size:11px;cursor:pointer">\u5220\u9664</button>'
      h += '</div>'
      h += '<div class="card-body" style="display:none;padding:8px 12px;border-top:1px solid var(--border);background:var(--bg-2);font-size:12px;color:var(--fg-2);white-space:pre-wrap">' + (m.body ? escH(m.body) : '<span style="color:var(--muted-2)">\u65e0\u5185\u5bb9</span>') + '</div>'
      h += '</div>'
    })
    h += '<div class="section"><div class="section-title">\u6dfb\u52a0\u8bb0\u5fc6</div>'
    h += '<div class="input-row" style="margin-bottom:8px"><label style="font-size:11px;color:var(--muted-2);display:block;margin-bottom:2px">\u540d\u79f0</label><input id="mem-name" type="text" placeholder="kebab-case-slug" style="width:100%;padding:5px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:12px"></div>'
    h += '<div class="input-row" style="margin-bottom:8px"><label style="font-size:11px;color:var(--muted-2);display:block;margin-bottom:2px">\u6807\u9898</label><input id="mem-title" type="text" placeholder="\u4eba\u53ef\u8bfb\u7684\u6807\u9898" style="width:100%;padding:5px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:12px"></div>'
    h += '<div class="input-row" style="margin-bottom:8px"><label style="font-size:11px;color:var(--muted-2);display:block;margin-bottom:2px">\u63cf\u8ff0</label><input id="mem-desc" type="text" placeholder="\u4e00\u884c\u6982\u8ff0" style="width:100%;padding:5px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:12px"></div>'
    h += '<div class="input-row" style="margin-bottom:8px"><label style="font-size:11px;color:var(--muted-2);display:block;margin-bottom:2px">\u7c7b\u578b</label><select id="mem-type" style="width:100%;padding:5px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:12px"><option value="user">user</option><option value="feedback">feedback</option><option value="project">project</option><option value="reference">reference</option></select></div>'
    if (isArch) {
      h += '<div class="input-row" style="margin-bottom:8px"><label style="font-size:11px;color:var(--muted-2);display:block;margin-bottom:2px">\u8303\u56f4</label><select id="mem-scope" style="width:100%;padding:5px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:12px"><option value="private">\u79c1\u6709\uff08\u4ec5\u81ea\u5df1\u53ef\u89c1\uff09</option><option value="global">\u5168\u5c40\uff08\u56e2\u961f\u5171\u4eab\uff09</option></select></div>'
    }
    h += sensSelect("mem-sens")
    h += '<div style="margin-bottom:8px"><label style="font-size:11px;color:var(--muted-2);display:block;margin-bottom:2px">\u5185\u5bb9</label><textarea id="mem-body" style="min-height:100px;width:100%;padding:6px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:12px;font-family:var(--mono)" placeholder="Markdown \u683c\u5f0f..."></textarea></div>'
    h += '<div style="text-align:right"><button class="btn primary" onclick="addMemory()" style="padding:6px 16px;border:none;border-radius:4px;background:var(--accent);color:#000;font-size:12px;cursor:pointer">\u4fdd\u5b58\u8bb0\u5fc6</button></div></div>'
  } catch (e) {
    h += '<div style="color:#f44336;padding:12px">\u52a0\u8f7d\u5931\u8d25</div>'
  }
  h += '</div>'
  contentHtml.value = h
}

// 安全 tab：机密清单（dirs/files）可视化编辑，仅 architect 可写。
async function renderSensitive() {
  const q = tokenQuery()
  contentHtml.value = '<div class="section"><h3>🛡 安全</h3><p class="desc">机密清单：AI 工具试图访问以下目录/文件时将直接拦截（不读取内容）。修改后下次新会话/切模型生效。</p></div><div id="sensitive-render">加载中...</div>'
  try {
    let role = ""
    try {
      const rr = await fetch("/teamix/user/role" + q)
      role = ((await rr.json()).role || "") as string
    } catch (e) { }
    const isArch = role === "architect"
    const resp = await fetch("/teamix/sensitive" + q)
    const data = await resp.json()
    const dirs: string[] = data.dirs || []
    const files: string[] = data.files || []
    const taStyle = 'width:100%;min-height:96px;padding:8px 10px;border:1px solid var(--border);border-radius:6px;background:var(--bg);color:var(--fg);font-size:12px;font-family:var(--mono);box-sizing:border-box;resize:vertical;line-height:1.6'
    const labStyle = 'display:block;font-size:12px;color:var(--fg);font-weight:500;margin:0 0 4px'
    const hintStyle = 'font-size:11px;color:var(--muted-2);font-weight:400;margin-left:6px'
    let h = '<div class="section"><h3>🛡 安全</h3><p class="desc">机密清单：AI 工具试图访问以下目录/文件时将直接拦截（不读取内容）。修改后下次新会话/切模型生效。</p></div>'
    h += '<div style="margin-bottom:14px"><label style="' + labStyle + '">机密目录<span style="' + hintStyle + '">每行一个，前缀匹配，如 tenders/ data/ secrets/</span></label><textarea id="sens-dirs" style="' + taStyle + '" placeholder="tenders/">' + escH(dirs.join("\n")) + '</textarea></div>'
    h += '<div style="margin-bottom:14px"><label style="' + labStyle + '">机密文件<span style="' + hintStyle + '">每行一个，glob 匹配，如 .env *.pem</span></label><textarea id="sens-files" style="' + taStyle + '" placeholder=".env">' + escH(files.join("\n")) + '</textarea></div>'
    if (isArch) {
      h += '<div style="display:flex;justify-content:flex-end;align-items:center;gap:10px;margin-top:4px"><button class="btn primary" onclick="saveSensitive()" style="padding:7px 20px;border:none;border-radius:6px;background:var(--accent);color:#000;font-size:12px;font-weight:500;cursor:pointer">保存机密清单</button><span id="sens-msg" style="font-size:12px;color:var(--muted-2)"></span></div>'
    } else {
      h += '<div style="color:var(--muted-2);font-size:12px;margin-top:8px">仅架构师可修改机密清单</div>'
    }
    contentHtml.value = h
  } catch (e: any) {
    contentHtml.value = '<div style="color:#f44336;padding:12px">加载失败: ' + e.message + '</div>'
  }
}


function personaCard(p: any, canEdit: boolean) {
  const scope = p.scope
  const activeBadge = p.effective
    ? '<span style="margin-left:6px;font-size:10px;padding:1px 6px;border-radius:99px;background:rgba(76,175,80,.15);color:#4caf50">\u5f53\u524d\u751f\u6548</span>'
    : ''
  const scopeBadge = scope === "global"
    ? '<span style="margin-left:6px;font-size:10px;padding:1px 6px;border-radius:99px;background:rgba(76,175,80,.15);color:#4caf50">\u5168\u5c40</span>'
    : '<span style="margin-left:6px;font-size:10px;padding:1px 6px;border-radius:99px;background:rgba(150,150,150,.15);color:var(--muted-2)">\u79c1\u6709</span>'
  const preview = p.systemPrompt ? p.systemPrompt.slice(0, 60).replace(/</g, "&lt;") : '<span style="color:var(--muted-2)">\u6682\u65e0\u5185\u5bb9</span>'
  let btns = ''
  if (!p.effective) {
    // "设为当前"只对个人生效：点全局=自己选全局人格（写 useGlobal），点私有=激活私有
    btns += '<button class="btn sm" data-soul-set="' + escAttr(p.name) + '" data-soul-scope="' + scope + '" style="padding:3px 10px;border:1px solid var(--accent);border-radius:4px;background:var(--accent-soft);color:var(--accent);font-size:11px;cursor:pointer">\u8bbe\u4e3a\u5f53\u524d</button>'
  }
  if (canEdit) {
    btns += '<button class="btn sm" data-soul-act="' + escAttr(p.name) + '" data-soul-scope="' + scope + '" style="padding:3px 10px;border:1px solid var(--border);border-radius:4px;background:var(--bg-2);color:var(--fg);font-size:11px;cursor:pointer">\u7f16\u8f91</button>'
    btns += '<button class="btn danger sm" data-soul-del="' + escAttr(p.name) + '" data-soul-scope="' + scope + '" style="padding:3px 10px;border:1px solid var(--danger);border-radius:4px;background:var(--danger-soft);color:var(--danger);font-size:11px;cursor:pointer">\u5220\u9664</button>'
  }
  return '<div class="card" style="flex-direction:column;align-items:stretch" data-open="false">'
    + '<div class="card-head" onclick="const c=this.closest(\'.card\');const b=c.querySelector(\'.card-body\');const o=c.dataset.open===\'true\';c.dataset.open=o?\'false\':\'true\';b.style.display=o?\'none\':\'\'">'
    + '<span class="chev" style="color:var(--muted-2);transition:transform .15s;display:inline-flex"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="6 9 12 15 18 9"/></svg></span>'
    + '<div class="card-main"><div class="card-title"><span class="name">' + escH(p.name) + '</span>' + scopeBadge + activeBadge + '</div>'
    + '<span class="subject">' + preview + '</span></div>'
    + '<div class="card-actions" style="display:flex;gap:6px;flex-shrink:0;align-items:center;flex-wrap:nowrap">' + btns + '</div>'
    + '</div>'
    + '<div class="card-body" style="display:none;padding:8px 12px;border-top:1px solid var(--border);background:var(--bg-2);font-size:12px;color:var(--fg-2);white-space:pre-wrap">' + (p.systemPrompt ? escH(p.systemPrompt) : '<span style="color:var(--muted-2)">\u6682\u65e0\u5185\u5bb9</span>') + '</div>'
    + '</div>'
}

async function renderSoul() {
  const q = tokenQuery()
  let h = '<div class="section"><h3>\ud83e\udde0 AI \u4eba\u683c</h3><p class="desc">\u4eba\u683c\u662f\u53ef\u547d\u540d\u7684\u7cfb\u7edf\u63d0\u793a\u8bcd\u6a21\u677f\u3002\u5168\u5c40\u4eba\u683c\u7531\u67b6\u6784\u5e08\u7ef4\u62a4\uff0c\u79c1\u6709\u4eba\u683c\u53ea\u5bf9\u81ea\u5df1\u53ef\u89c1\u3002\u540c\u65f6\u53ea\u80fd\u6709\u4e00\u4e2a\u751f\u6548\uff0c\u7528\u201c\u8bbe\u4e3a\u5f53\u524d\u201d\u9009\u62e9\u3002</p></div>'
  try {
    let role = ""
    try {
      const rr = await fetch("/teamix/user/role" + q)
      role = ((await rr.json()).role || "") as string
    } catch (e) { }
    const isArch = role === "architect"
    const resp = await fetch("/teamix/soul" + q)
    const data = await resp.json()
    const gps = (data && data.global && data.global.personas) || []
    const pps = (data && data.private && data.private.personas) || []
    const myGlobal = (data && data.private && data.private.useGlobal) || ""
    // 计算唯一生效人格（均为个人选择，无全局强制）：1) 私有人格 active 2) useGlobal 指定的全局人格
    let effKey = "" // "global:名字" 或 "private:名字"
    const privActive = pps.find((p: any) => p.active)
    if (privActive) effKey = "private:" + privActive.name
    else if (myGlobal) effKey = "global:" + myGlobal
    const all = (gps.map((p: any) => ({ ...p, scope: "global" })) as any[])
      .concat(pps.map((p: any) => ({ ...p, scope: "private" })))
      .map((p: any) => ({ ...p, effective: (p.scope + ":" + p.name) === effKey }))
    h += '<div class="section"><div class="section-title">\u4eba\u683c\u5217\u8868 (' + all.length + ') <span style="font-size:11px;color:var(--muted-2)">\u5168\u5c40\u4eba\u683c\u7531\u67b6\u6784\u5e08\u63d0\u4f9b\uff0c\u201c\u8bbe\u4e3a\u5f53\u524d\u201d\u53ea\u5bf9\u81ea\u5df1\u751f\u6548</span></div>'
    if (all.length === 0) h += '<div style="color:var(--muted-2);text-align:center;padding:12px;font-size:13px">\u5c1a\u65e0\u4eba\u683c\uff0c\u4e0b\u9762\u6dfb\u52a0\u4e00\u4e2a</div>'
    all.forEach((p: any) => {
      // 架构师：全局/私有都可编辑；普通用户：全局只读（可设当前）、私有可编辑
      const canEdit = isArch || p.scope === "private"
      h += personaCard(p, canEdit)
    })
    // 添加表单（合并）：scope 下拉 —— 架构师可选全局/私有，普通用户固定私有
    h += '<div class="section-title" style="margin-top:10px">\u65b0\u5efa\u4eba\u683c</div>'
    if (isArch) {
      h += '<div class="input-row" style="margin-bottom:8px"><label style="font-size:11px;color:var(--muted-2);display:block;margin-bottom:2px">\u8303\u56f4</label><select id="soul-scope" style="width:100%;padding:5px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:12px"><option value="private">\u79c1\u6709\uff08\u4ec5\u81ea\u5df1\u53ef\u89c1\uff09</option><option value="global">\u5168\u5c40\uff08\u5168\u5458\u53ef\u89c1\uff09</option></select></div>'
    } else {
      h += '<input type="hidden" id="soul-scope" value="private">'
    }
    h += '<div class="input-row" style="margin-bottom:8px"><label style="font-size:11px;color:var(--muted-2);display:block;margin-bottom:2px">\u540d\u79f0</label><input id="soul-name" type="text" placeholder="\u5982\uff1a\u6211\u7684\u5f00\u53d1\u98ce\u683c" style="width:100%;padding:5px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:12px"></div>'
    h += '<div class="input-row" style="margin-bottom:8px"><label style="font-size:11px;color:var(--muted-2);display:block;margin-bottom:2px">\u7cfb\u7edf\u63d0\u793a\u8bcd</label><textarea id="soul-prompt" style="min-height:120px;width:100%;padding:6px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:12px;font-family:var(--mono)"></textarea></div>'
    h += '<div style="text-align:right"><button class="btn primary" onclick="saveSoul()" style="padding:6px 16px;border:none;border-radius:4px;background:var(--accent);color:#000;font-size:12px;cursor:pointer">\u4fdd\u5b58\u4eba\u683c</button></div></div>'
  } catch (e) {
    h += '<div style="color:#f44336;padding:12px">\u52a0\u8f7d\u5931\u8d25</div>'
  }
  contentHtml.value = h
}

// AI 审计面板（仅架构师）：AI 调用操作流向 + 泄露三信号（出网/事故/告警）。
async function renderAudit() {
  const q = tokenQuery()
  let h = '<div class="section"><h3>🛡 AI 审计</h3><p class="desc">AI 调用操作流向：每次模型请求走了哪个模型、是否出网、如何脱敏（仅架构师）。红色 = 泄露事故（非 public 却出网 / 闭环检测命中）。</p></div>'
  const inpStyle = 'padding:6px 10px;font-size:12px;border:1px solid var(--border);border-radius:6px;background:var(--bg);color:var(--fg);outline:none'
  h += '<div style="display:flex;gap:8px;align-items:center;flex-wrap:wrap;margin-bottom:10px">'
  h += '<input id="audit-user" placeholder="用户（空=全部）" style="' + inpStyle + ';width:120px">'
  h += '<input id="audit-date" placeholder="日期 YYYY-MM-DD" style="' + inpStyle + ';width:140px">'
  h += '<label style="font-size:12px;color:var(--fg);display:flex;align-items:center;gap:4px;cursor:pointer"><input type="checkbox" id="audit-outbound"> 只看出了网的</label>'
  h += '<label style="font-size:12px;color:var(--fg);display:flex;align-items:center;gap:4px;cursor:pointer"><input type="checkbox" id="audit-alert"> 只看告警</label>'
  h += '<button class="btn primary" onclick="auditLoad()" style="margin-left:auto;padding:6px 18px;border:none;border-radius:6px;background:var(--accent);color:#000;font-size:12px;font-weight:500;cursor:pointer">查询</button>'
  h += '</div><div id="audit-render" style="margin-top:8px">'
  h += await auditRows(q, "", "", false, false)
  h += '</div>'
  contentHtml.value = h
  ;(window as any).auditLoad = async function () {
    const u = (document.getElementById("audit-user") as HTMLInputElement)?.value || ""
    const d = (document.getElementById("audit-date") as HTMLInputElement)?.value || ""
    const ob = (document.getElementById("audit-outbound") as HTMLInputElement)?.checked || false
    const al = (document.getElementById("audit-alert") as HTMLInputElement)?.checked || false
    const box = document.getElementById("audit-render")
    if (box) box.innerHTML = await auditRows(q, u, d, ob, al)
  }
}


async function auditRows(q: string, user: string, date: string, outbound: boolean, alert: boolean): Promise<string> {
  const params = new URLSearchParams()
  if (user) params.set("user", user)
  if (date) params.set("date", date)
  if (outbound) params.set("outbound", "true")
  if (alert) params.set("alert", "true")
  const sep = q.includes("?") ? "&" : "?"
  try {
    const resp = await fetch("/teamix/audit/ai-logs" + q + (params.toString() ? sep + params.toString() : ""))
    if (!resp.ok) return '<div style="color:#f44336;padding:12px;font-size:13px">无权访问或加载失败</div>'
    const data = await resp.json()
    const recs = data.records || []
    if (recs.length === 0) return '<div style="color:var(--muted-2);text-align:center;padding:16px;font-size:13px">暂无记录</div>'
    let h = ''
    for (const r of recs) {
      const critical = (r.alerts || []).some((a: string) => a.includes("[critical]"))
      const sent = r.outbound?.sent
      const sens = r.sensitivity || "未标记"
      const sentBadge = sent
        ? '<span style="font-size:10px;padding:1px 6px;border-radius:99px;background:rgba(76,175,80,.15);color:#4caf50;margin-left:8px">出网</span>'
        : '<span style="font-size:10px;padding:1px 6px;border-radius:99px;background:rgba(150,150,150,.15);color:var(--muted-2);margin-left:8px">内网</span>'
      const sensColor = sens === "public" ? '#4caf50' : sens === "redact" ? '#2196f3' : '#ff9800'
      const sensBadge = sens !== "未标记"
        ? '<span style="font-size:10px;padding:1px 6px;border-radius:99px;background:rgba(128,128,128,.12);color:' + sensColor + ';margin-left:6px">' + escH(sens) + '</span>'
        : ''
      const trace = (r.trace || []).map((s: any) => `${s.step}:${s.model || ""}(${s.reason || ""})`).join(" → ")
      h += '<div style="border:1px solid ' + (critical ? "rgba(244,67,54,.5)" : "var(--border,rgba(128,128,128,.2))") + ';border-radius:8px;padding:10px 12px;margin-bottom:8px;font-size:12px;line-height:1.6;background:' + (critical ? "rgba(244,67,54,.05)" : "var(--bg-2)") + '">'
      h += '<div style="display:flex;align-items:center;flex-wrap:wrap"><b>' + escH(r.time ? new Date(r.time).toLocaleString() : "") + '</b>'
      h += '<span style="margin-left:8px;color:var(--muted-2)">' + escH(r.user || "") + '</span>'
      h += '<span style="margin-left:8px;color:var(--fg)">' + escH(r.purpose || "") + '</span>'
      h += sentBadge + sensBadge
      h += '</div>'
      h += '<div style="color:var(--muted-2);margin-top:4px;font-size:11px;word-break:break-all">' + escH(trace) + '</div>'
      if (r.alerts && r.alerts.length) h += '<div style="color:' + (critical ? "#f44336" : "#e6a23c") + ';margin-top:4px;font-weight:500">⚠ ' + escH(r.alerts.join("; ")) + '</div>'
      h += '</div>'
    }
    return h
  } catch (e: any) {
    return '<div style="color:#f44336;padding:12px;font-size:13px">加载失败: ' + escH(e.message) + '</div>'
  }
}

function tokenQuery() {
  const t = localStorage.getItem("teamix_token")
  if (!t) return ""
  return "?token=" + encodeURIComponent(t)
}
function escH(s: any) { return String(s).replace(/</g, "&lt;").replace(/>/g, "&gt;") }
function escAttr(s: any) { return String(s).replace(/&/g, "&amp;").replace(/"/g, "&quot;").replace(/</g, "&lt;").replace(/>/g, "&gt;") }

// Global functions needed by inline onclick handlers
const w = window as any
w.saveSensitive = async function() {
  const dirsText = (document.getElementById("sens-dirs") as HTMLTextAreaElement)?.value || ""
  const filesText = (document.getElementById("sens-files") as HTMLTextAreaElement)?.value || ""
  const dirs = dirsText.split("\n").map(s => s.trim()).filter(s => s && !s.startsWith("#"))
  const files = filesText.split("\n").map(s => s.trim()).filter(s => s && !s.startsWith("#"))
  const t = localStorage.getItem("teamix_token")
  if (!t) return
  const resp = await fetch("/teamix/sensitive?token=" + encodeURIComponent(t), {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ dirs, files })
  })
  const msg = document.getElementById("sens-msg")
  if (resp.ok) {
    if (msg) msg.textContent = "\u2713 \u5df2\u4fdd\u5b58\uff08\u4e0b\u6b21\u65b0\u4f1a\u8bdd/\u5207\u6a21\u578b\u751f\u6548\uff09"
    toast("机密清单已保存")
  } else {
    if (msg) msg.textContent = "\u2717 \u4fdd\u5b58\u5931\u8d25"
  }
}
w.saveKeyStrategy = async function() {
  const sel = document.getElementById("key-strategy-select") as HTMLSelectElement
  if (!sel) return
  const t = localStorage.getItem("teamix_token")
  if (!t) return
  await fetch("/teamix/keypool/strategy?token=" + encodeURIComponent(t), {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ strategy: sel.value })
  })
}
function deleteKey(envName: string) {
  if (!envName) return
  const t = localStorage.getItem("teamix_token")
  if (!t) return
  fetch("/teamix/secrets/delete?token=" + encodeURIComponent(t), {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ envName })
  }).then(() => { refreshTab("keys") })
}
w.addKey = async function() {
  const env = document.getElementById("new-key-env") as HTMLInputElement
  const val = document.getElementById("new-key-value") as HTMLInputElement
  if (!val || !val.value.trim()) return
  const t = localStorage.getItem("teamix_token")
  if (!t) return
  await fetch("/teamix/secrets/set?token=" + encodeURIComponent(t), {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ envName: env ? env.value.trim() : "", value: val.value.trim() })
  })
  tab.value = "keys"
  await refreshTab("keys")
}
w.addMCPServer = async function() {
  const name = (document.getElementById("mcp-name") as HTMLInputElement)?.value.trim()
  const cmd = (document.getElementById("mcp-cmd") as HTMLInputElement)?.value.trim()
  const transport = (document.getElementById("mcp-transport") as HTMLSelectElement)?.value
  const args = (document.getElementById("mcp-args") as HTMLInputElement)?.value.trim()
  const scopeSel = document.getElementById("mcp-scope") as HTMLSelectElement
  const scope = scopeSel ? scopeSel.value : "private"
  const sensSel = document.getElementById("mcp-sens") as HTMLSelectElement
  const sensitivity = sensSel ? sensSel.value : "internal"
  if (!name || !cmd) return
  const t = localStorage.getItem("teamix_token")
  if (!t) return
  await fetch("/teamix/mcp/add?token=" + encodeURIComponent(t), {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name, command: cmd, transport, args, scope, sensitivity })
  })
  tab.value = "mcp"
  await refreshTab("mcp")
}
function removeMCPServer(name: string) {
  if (!name) return
  const t = localStorage.getItem("teamix_token")
  if (!t) return
  fetch("/teamix/mcp/remove?token=" + encodeURIComponent(t), {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name })
  }).then(() => { refreshTab("mcp") })
}
// 删除类按钮事件委托（避免内联 onclick 拼接名字导致的注入）
// 刷新指定 tab：若已在当前 tab（Vue watch 不触发），显式重新加载渲染。
async function refreshTab(t: string) {
  if (tab.value === t) {
    await switchTab(t)
  } else {
    tab.value = t
  }
}
function postJSON(path: string, body: any) {
  const t = localStorage.getItem("teamix_token")
  if (!t) return Promise.resolve()
  return fetch(path + "?token=" + encodeURIComponent(t), {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body)
  }).catch(() => {})
}
document.addEventListener("click", (ev) => {
  const target = ev.target as HTMLElement
  const mcpBtn = target.closest("[data-mcp-remove]") as HTMLElement | null
  if (mcpBtn) {
    ev.preventDefault()
    const name = mcpBtn.getAttribute("data-mcp-remove")
    if (name) removeMCPServer(name)
    return
  }
  const keyBtn = target.closest("[data-key-del]") as HTMLElement | null
  if (keyBtn) {
    ev.preventDefault()
    const env = keyBtn.getAttribute("data-key-del")
    if (env) deleteKey(env)
    return
  }
  const skillBtn = target.closest("[data-skill-del]") as HTMLElement | null
  if (skillBtn) {
    ev.preventDefault()
    const name = skillBtn.getAttribute("data-skill-del")
    const scope = skillBtn.getAttribute("data-skill-scope") || "private"
    if (name) {
      postJSON("/teamix/skills/delete", { name, scope }).then(() => { refreshTab("skills") })
    }
    return
  }
  const memBtn = target.closest("[data-mem-del]") as HTMLElement | null
  if (memBtn) {
    ev.preventDefault()
    const name = memBtn.getAttribute("data-mem-del")
    if (name) {
      postJSON("/teamix/memory/delete", { name, scope: "private" }).then(() => { refreshTab("memory") })
    }
    return
  }
  const memGBtn = target.closest("[data-mem-del-global]") as HTMLElement | null
  if (memGBtn) {
    ev.preventDefault()
    const name = memGBtn.getAttribute("data-mem-del-global")
    if (name) {
      postJSON("/teamix/memory/delete", { name, scope: "global" }).then(() => { refreshTab("memory") })
    }
    return
  }
  const soulDelBtn = target.closest("[data-soul-del]") as HTMLElement | null
  if (soulDelBtn) {
    ev.preventDefault()
    const name = soulDelBtn.getAttribute("data-soul-del")
    const scope = soulDelBtn.getAttribute("data-soul-scope") || "private"
    if (name) {
      postJSON("/teamix/soul/delete", { name, scope }).then(() => { refreshTab("soul") })
    }
    return
  }
  const soulSetBtn = target.closest("[data-soul-set]") as HTMLElement | null
  if (soulSetBtn) {
    ev.preventDefault()
    const name = soulSetBtn.getAttribute("data-soul-set")
    const scope = soulSetBtn.getAttribute("data-soul-scope") || "private"
    if (name) {
      // "设为当前"只对个人生效：全局→写自己 useGlobal，私有→激活私有（后端已互斥处理）
      postJSON("/teamix/soul/activate", { name, scope }).then(() => { refreshTab("soul") })
    }
    return
  }
  const soulUseBtn = target.closest("[data-soul-use]") as HTMLElement | null
  if (soulUseBtn) {
    ev.preventDefault()
    const name = soulUseBtn.getAttribute("data-soul-use") || ""
    // 空 name = 取消选择（恢复全局默认）；非空 = 使用该全局人格（只影响自己）
    postJSON("/teamix/soul/use", { name }).then(() => { refreshTab("soul") })
    return
  }
  const soulActBtn = target.closest("[data-soul-act]") as HTMLElement | null
  if (soulActBtn) {
    ev.preventDefault()
    const name = soulActBtn.getAttribute("data-soul-act")
    const scope = soulActBtn.getAttribute("data-soul-scope") || "private"
    if (name) editSoul(name, scope)
    return
  }
  const userBtn = target.closest("[data-user-del]") as HTMLElement | null
  if (userBtn) {
    ev.preventDefault()
    const name = userBtn.getAttribute("data-user-del")
    if (name) removeUser(name)
    return
  }
  const uEditBtn = target.closest("[data-user-edit]") as HTMLElement | null
  if (uEditBtn) {
    ev.preventDefault()
    const name = uEditBtn.getAttribute("data-user-edit")
    const box = name ? document.getElementById("user-edit-" + name) : null
    if (box) {
            const showing = box.style.display !== "none" && box.style.display !== ""
      // 首次展开时回显已有凭证
      if (!showing && name) { fetch("/teamix/users/credentials?name=" + encodeURIComponent(name) + "&token=" + encodeURIComponent(localStorage.getItem("teamix_token") || "")).then(r => r.json()).then(data => { const uEl = document.getElementById("edit-uuser-" + name); const pEl = document.getElementById("edit-upass-" + name); if (uEl && data.httpsUsername) uEl.value = data.httpsUsername; if (pEl && data.configured) pEl.placeholder = "(已设置，留空则不变)" }).catch(() => {}) }
      box.style.display = showing ? "none" : "block"
      uEditBtn.textContent = showing ? "编辑" : "取消"
    }
    return
  }
  const uSaveBtn = target.closest("[data-user-save-cred]") as HTMLElement | null
  if (uSaveBtn) {
    ev.preventDefault()
    const name = uSaveBtn.getAttribute("data-user-save-cred")
    if (name) saveUserCredentials(name)
  }
  const projBtn = target.closest("[data-proj-del]") as HTMLElement | null
  if (projBtn) {
    ev.preventDefault()
    const name = projBtn.getAttribute("data-proj-del")
    if (name) removeProject(name)
    return
  }
  const scanBtn = target.closest("[data-proj-scan]") as HTMLElement | null
  if (scanBtn) {
    ev.preventDefault()
    const name = scanBtn.getAttribute("data-proj-scan")
    if (name) {
      const bar = document.getElementById("proj-bar-" + name)
      if (bar) bar.style.display = "flex"
      postJSON("/teamix/projects/" + encodeURIComponent(name) + "/scan", {}).finally(() => {
        if (bar) bar.style.display = "none"
        refreshTab("projects")
      })
    }
    return
  }
  const expBtn = target.closest("[data-proj-expand]") as HTMLElement | null
  if (expBtn) {
    ev.preventDefault()
    const name = expBtn.getAttribute("data-proj-expand")
    if (name) toggleProjectServices(name, expBtn)
    return
  }
  const editBtn = target.closest("[data-proj-edit]") as HTMLElement | null
  if (editBtn) {
    ev.preventDefault()
    const name = editBtn.getAttribute("data-proj-edit")
    const box = name ? document.getElementById("proj-edit-" + name) : null
    if (box) {
            const showing = box.style.display !== "none" && box.style.display !== ""
      // 首次展开时回显已有凭证
      if (!showing && name) { fetch("/teamix/users/credentials?name=" + encodeURIComponent(name) + "&token=" + encodeURIComponent(localStorage.getItem("teamix_token") || "")).then(r => r.json()).then(data => { const uEl = document.getElementById("edit-uuser-" + name); const pEl = document.getElementById("edit-upass-" + name); if (uEl && data.httpsUsername) uEl.value = data.httpsUsername; if (pEl && data.configured) pEl.placeholder = "(已设置，留空则不变)" }).catch(() => {}) }
      box.style.display = showing ? "none" : "block"
      editBtn.textContent = showing ? "编辑" : "取消"
    }
    return
  }
  const saveBtn = target.closest("[data-proj-save]") as HTMLElement | null
  if (saveBtn) {
    ev.preventDefault()
    const name = saveBtn.getAttribute("data-proj-save")
    if (name) saveProjectEdit(name)
  }
})
// 角色切换下拉 change 事件委托
document.addEventListener("change", (ev) => {
  const sel = (ev.target as HTMLElement).closest("[data-user-role]") as HTMLSelectElement | null
  if (sel) {
    const name = sel.getAttribute("data-user-role")
    if (name) changeUserRole(name, sel.value)
  }
})
w.toggleSkill = async function(name: string, checked: boolean) {
  const t = localStorage.getItem("teamix_token")
  if (!t) return
  await fetch("/teamix/skills/toggle?token=" + encodeURIComponent(t), {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name, enabled: checked })
  })
}
w.addMemory = async function() {
  const name = (document.getElementById("mem-name") as HTMLInputElement)?.value.trim()
  const title = (document.getElementById("mem-title") as HTMLInputElement)?.value.trim()
  const desc = (document.getElementById("mem-desc") as HTMLInputElement)?.value.trim()
  const mtype = (document.getElementById("mem-type") as HTMLSelectElement)?.value
  const scopeSel = document.getElementById("mem-scope") as HTMLSelectElement
  const scope = scopeSel ? scopeSel.value : "private"
  const sensSel = document.getElementById("mem-sens") as HTMLSelectElement
  const sensitivity = sensSel ? sensSel.value : "internal"
  const body = (document.getElementById("mem-body") as HTMLTextAreaElement)?.value
  if (!name) return
  const t = localStorage.getItem("teamix_token")
  if (!t) return
  await fetch("/teamix/memory/save?token=" + encodeURIComponent(t), {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name, title, description: desc, type: mtype || "user", sensitivity, body: body || "", scope })
  })
  tab.value = "memory"
  await refreshTab("memory")
}
w.addUser = async function() {
  const name = (document.getElementById("user-name") as HTMLInputElement)?.value.trim()
  const role = (document.getElementById("user-role") as HTMLSelectElement)?.value || "developer"
  const httpsUser = (document.getElementById("user-https-user") as HTMLInputElement)?.value.trim() || ""
  const httpsPass = (document.getElementById("user-https-pass") as HTMLInputElement)?.value || ""
  if (!name) return
  const t = localStorage.getItem("teamix_token")
  if (!t) return
  await fetch("/teamix/users/add?token=" + encodeURIComponent(t), {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name, role, httpsUsername: httpsUser, httpsPassword: httpsPass })
  })
  await refreshTab("users")
}
function changeUserRole(name: string, role: string) {
  if (!name) return
  const t = localStorage.getItem("teamix_token")
  if (!t) return
  fetch("/teamix/users/role?token=" + encodeURIComponent(t), {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name, role })
  }).then(() => { refreshTab("users") })
}
function removeUser(name: string) {
  if (!name) return
  const t = localStorage.getItem("teamix_token")
  if (!t) return
  fetch("/teamix/users/remove?token=" + encodeURIComponent(t), {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name })
  }).then(() => { refreshTab("users") })
}

// 为已存在用户补/改 git 凭证（账号或访问令牌）
function saveUserCredentials(name: string) {
  const user = (document.getElementById("edit-uuser-" + name) as HTMLInputElement)?.value.trim()
  const pass = (document.getElementById("edit-upass-" + name) as HTMLInputElement)?.value || ""
  if (!user) {
    toast("请输入账号/令牌用户名", "error")
    return
  }
  postJSON("/teamix/users/credentials", { name, httpsUsername: user, httpsPassword: pass }).then(() => {
    toast("凭证已保存", "success")
    refreshTab("users")
  })
}
w.addProject = async function() {
  const name = (document.getElementById("proj-name") as HTMLInputElement)?.value.trim()
  const git = (document.getElementById("proj-git") as HTMLInputElement)?.value.trim()
  const desc = (document.getElementById("proj-desc") as HTMLInputElement)?.value.trim()
  const errEl = document.getElementById("proj-err")
  if (!name || !git) { if (errEl) errEl.textContent = "请填写项目名与 git 链接"; return }
  const bar = document.getElementById("proj-add-bar")
  if (errEl) errEl.textContent = ""
  if (bar) bar.style.display = "flex"
  const t = localStorage.getItem("teamix_token")
  if (!t) return
  try {
    const resp = await fetch("/teamix/projects/add?token=" + encodeURIComponent(t), {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name, git, description: desc })
    })
    const data = await resp.json()
    if (data && data.ok === false) {
      if (errEl) errEl.textContent = data.error || "添加失败"
      return
    }
    await refreshTab("projects")
  } catch (e: any) {
    if (errEl) errEl.textContent = "添加失败: " + String(e)
  } finally {
    if (bar) bar.style.display = "none"
  }
}
function removeProject(name: string) {
  if (!name) return
  const t = localStorage.getItem("teamix_token")
  if (!t) return
  fetch("/teamix/projects/remove?token=" + encodeURIComponent(t), {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name })
  }).then(() => { refreshTab("projects") })
}

// 保存项目编辑（git 链接/描述）；改链接会同步公共区 remote。
function saveProjectEdit(name: string) {
  const git = (document.getElementById("edit-git-" + name) as HTMLInputElement)?.value.trim()
  const desc = (document.getElementById("edit-desc-" + name) as HTMLInputElement)?.value.trim()
  const errEl = document.getElementById("proj-err")
  if (!git) { if (errEl) errEl.textContent = "git 链接不能为空"; return }
  if (errEl) errEl.textContent = ""
  postJSON("/teamix/projects/update", { name, git, description: desc }).then(() => { refreshTab("projects") })
}

// 展开/收起项目的服务明细（首次展开拉取 /teamix/projects/{name}/services）。
function toggleProjectServices(name: string, btn: HTMLElement) {
  const box = document.getElementById("proj-svc-" + name)
  if (!box) return
  if (box.style.display === "none" || box.style.display === "") {
    box.style.display = "block"
    btn.textContent = "收起"
    if (box.innerHTML === "") {
      api.projectServices(name).then((list: any[]) => {
        if (!Array.isArray(list) || list.length === 0) {
          box.innerHTML = '<div class="cfg-svc-empty">该项目未配置服务（可点\u201c重新扫描\u201d识别模块）</div>'
          return
        }
        let h = '<div class="cfg-svc-head">共 ' + list.length + ' 个服务</div>'
        list.forEach((s: any) => {
          h += '<div class="cfg-svc-row"><span class="cfg-svc-name">' + escH(s.name) + '</span>'
          h += '<span class="cfg-svc-type">' + escH(s.type || "-") + '</span>'
          h += '<span class="cfg-svc-port">' + (s.port ? ":" + s.port : "") + '</span>'
          h += '<span class="cfg-svc-dir">' + escH(s.dir || "") + '</span>'
          h += '<span class="cfg-svc-startup">' + escH(s.startup || "") + '</span></div>'
        })
        box.innerHTML = h
      }).catch(() => {
        box.innerHTML = '<div class="cfg-svc-empty">加载失败</div>'
      })
    }
  } else {
    box.style.display = "none"
    btn.textContent = "展开"
  }
}
w.addSkill = async function() {
  const name = (document.getElementById("skill-name") as HTMLInputElement)?.value.trim()
  const desc = (document.getElementById("skill-desc") as HTMLInputElement)?.value.trim()
  const scopeSel = document.getElementById("skill-scope") as HTMLSelectElement
  const scope = scopeSel ? scopeSel.value : "private"
  const sensSel = document.getElementById("skill-sens") as HTMLSelectElement
  const sensitivity = sensSel ? sensSel.value : "internal"
  const body = (document.getElementById("skill-body") as HTMLTextAreaElement)?.value
  if (!name) return
  const t = localStorage.getItem("teamix_token")
  if (!t) return
  await fetch("/teamix/skills/save?token=" + encodeURIComponent(t), {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name, description: desc, sensitivity, body: body || "", scope })
  })
  tab.value = "skills"
  await refreshTab("skills")
}
w.saveSoul = async function() {
  const scopeEl = document.getElementById("soul-scope") as HTMLSelectElement
  const nameEl = document.getElementById("soul-name") as HTMLInputElement
  const prEl = document.getElementById("soul-prompt") as HTMLTextAreaElement
  if (!nameEl || !prEl) return
  const s = scopeEl ? scopeEl.value : "private"
  const name = nameEl.value.trim()
  if (!name) { toast("\u8bf7\u8f93\u5165\u4eba\u683c\u540d\u79f0"); return }
  const t = localStorage.getItem("teamix_token")
  if (!t) return
  try {
    const resp = await fetch("/teamix/soul/save?token=" + encodeURIComponent(t), {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ scope: s, name, systemPrompt: prEl.value, activate: false })
    })
    if (!resp.ok) {
      let msg = "HTTP " + resp.status
      try { msg = ((await resp.json()).error) || msg } catch (e) { }
      toast("\u4fdd\u5b58\u4eba\u683c\u5931\u8d25: " + msg)
      return
    }
    toast((s === "global" ? "\u5168\u5c40" : "\u79c1\u6709") + "\u4eba\u683c\u300c" + name + "\u300d\u5df2\u4fdd\u5b58\uff08\u672a\u751f\u6548\uff0c\u53ef\u70b9\u201c\u8bbe\u4e3a\u5f53\u524d\u201d\uff09", "success")
    tab.value = "soul"
    await refreshTab("soul")
  } catch (e: any) {
    toast("\u4fdd\u5b58\u4eba\u683c\u5931\u8d25: " + (e.message || e))
  }
}

// editSoul 将指定人格回填到添加表单，供修改后保存（同名覆盖更新）。
async function editSoul(name: string, scope: string) {
  const q = tokenQuery()
  try {
    const resp = await fetch("/teamix/soul" + q)
    const data = await resp.json()
    const list = (scope === "global" ? data.global : data.private) || { personas: [] }
    const p = (list.personas || []).find((x: any) => x.name === name)
    if (!p) return
    const scopeEl = document.getElementById("soul-scope") as HTMLSelectElement
    const nameEl = document.getElementById("soul-name") as HTMLInputElement
    const prEl = document.getElementById("soul-prompt") as HTMLTextAreaElement
    if (scopeEl) scopeEl.value = scope
    if (nameEl && prEl) {
      nameEl.value = p.name
      prEl.value = p.systemPrompt || ""
      nameEl.scrollIntoView({ behavior: "smooth", block: "center" })
    }
  } catch (e) { }
}
w.switchSettingsTab = function(t: string) { tab.value = t }
</script>

<template>
  <div class="modal-overlay" v-if="visible" @click.self="emit('close')" style="z-index:200">
    <div class="modal" style="width:min(780px,90vw);height:65vh;display:flex;flex-direction:column">
      <div class="modal__head" style="flex-shrink:0">
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/></svg>
        <span>项目配置</span>
        <span class="modal__close" @click="emit('close')">&times;</span>
      </div>
      <div style="display:flex;flex:1;min-height:0;overflow:hidden">
        <div style="width:140px;flex-shrink:0;border-right:1px solid var(--border);padding:8px">
          <div v-for="t in visibleTabs" :key="t"
            class="settings-tab" :class="{ active: tab === t }"
            @click="tab = t"
            style="padding:6px 10px;border-radius:6px;cursor:pointer;font-size:13px;margin-bottom:2px;display:flex;align-items:center;gap:6px">
            <span>{{ tabIcon[t] }}</span>
            <span>{{ tabLbl[t] }}</span>
          </div>
        </div>
        <div class="settings-content" style="flex:1;overflow-y:auto;padding:12px;font-size:13px;color:var(--muted)">
          <div v-html="contentHtml"></div>
        </div>
      </div>
    </div>
  </div>
</template>
