<script setup lang="ts">
import { ref, watch, onMounted } from "vue"
import { api } from "../api"
const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{ (e: "close"): void }>()
const isArch = ref(false)
const tab = ref("keys")
const allTabs = ["users", "projects", "keys", "mcp", "soul", "skills", "memory"]
// 普通用户只可见自己私有相关的配置（MCP/Skills/记忆），用户/项目/密钥池/AI 人格为架构师专属
const visibleTabs = ref<string[]>(allTabs)
const tabLbl: Record<string, string> = { users: "\u7528\u6237", projects: "\u9879\u76ee", keys: "\u5bc6\u94a5\u6c60", mcp: "MCP", soul: "AI \u4eba\u683c", skills: "Skills", memory: "\u8bb0\u5fc6" }
const tabIcon: Record<string, string> = { users: "\ud83d\udc65", projects: "\ud83d\udce6", keys: "\ud83d\udd11", mcp: "\ud83d\udd27", soul: "\ud83e\udde0", skills: "\ud83d\udcdc", memory: "\ud83e\udde0" }

onMounted(async () => {
  try {
    const r = await api.userRole()
    isArch.value = r.role === "architect"
  } catch {}
  visibleTabs.value = isArch.value ? allTabs : ["mcp", "skills", "memory"]
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
    else { await renderCapability(t) }
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
      h += '<div class="card" style="flex-direction:row;align-items:center;justify-content:space-between;padding:8px 12px">'
      h += '<div class="card-info"><div class="card-title"><code>' + escH(u.name) + '</code>' + cur + '</div></div>'
      h += '<div style="display:flex;gap:6px;align-items:center">'
      h += '<select data-user-role="' + escAttr(u.name) + '" style="padding:4px 6px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:12px"><option value="developer"' + (u.role === 'developer' ? ' selected' : '') + '>developer</option><option value="architect"' + (u.role === 'architect' ? ' selected' : '') + '>architect</option></select>'
      h += '<button class="btn danger sm" data-user-del="' + escAttr(u.name) + '" style="padding:4px 10px;border:1px solid var(--danger);border-radius:4px;background:var(--danger-soft);color:var(--danger);font-size:11px;cursor:pointer">\u5220\u9664</button>'
      h += '</div></div>'
    })
    h += '<div class="section"><div class="section-title">\u6dfb\u52a0\u7528\u6237</div>'
    h += '<div style="display:flex;gap:8px;align-items:flex-end;margin-bottom:8px">'
    h += '<div style="flex:1"><label style="font-size:11px;color:var(--muted-2);display:block;margin-bottom:2px">\u6635\u79f0</label><input id="user-name" type="text" placeholder="\u5982 alice" style="width:100%;padding:5px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:12px"></div>'
    h += '<div style="flex:1"><label style="font-size:11px;color:var(--muted-2);display:block;margin-bottom:2px">\u89d2\u8272</label><select id="user-role" style="width:100%;padding:5px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:12px"><option value="developer">developer</option><option value="architect">architect</option></select></div>'
    h += '<button class="btn primary" onclick="addUser()" style="padding:6px 16px;border:none;border-radius:4px;background:var(--accent);color:#000;font-size:12px;cursor:pointer">\u6dfb\u52a0</button></div></div>'
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
      h += '<button class="btn sm" data-proj-scan="' + escAttr(p.name) + '" style="padding:4px 10px;border:1px solid var(--border);border-radius:4px;background:var(--bg-2);color:var(--fg);font-size:11px;cursor:pointer">\u91cd\u65b0\u626b\u63cf</button>'
      h += '<button class="btn danger sm" data-proj-del="' + escAttr(p.name) + '" style="padding:4px 10px;border:1px solid var(--danger);border-radius:4px;background:var(--danger-soft);color:var(--danger);font-size:11px;cursor:pointer">\u5220\u9664</button></div>'
      h += '</div>'
      h += '<div class="cfg-svc-list" id="proj-svc-' + escAttr(p.name) + '" style="display:none"></div>'
      h += '<div class="cfg-progress" id="proj-bar-' + escAttr(p.name) + '" style="display:none"><div class="cfg-progress__bar"></div><span>\u6b63\u5728\u62c9\u53d6\u4ee3\u7801\u5e76\u626b\u63cf\u6a21\u5757...</span></div>'
    })
    h += '<div class="section"><div class="section-title">\u6dfb\u52a0\u9879\u76ee</div>'
    h += '<div id="proj-err" style="color:var(--danger);font-size:12px;margin-bottom:6px"></div>'
    h += '<div style="display:flex;gap:8px;margin-bottom:8px"><div style="flex:1"><label style="font-size:11px;color:var(--muted-2);display:block;margin-bottom:2px">\u540d\u79f0</label><input id="proj-name" type="text" placeholder="mall-system" style="width:100%;padding:5px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:12px"></div><div style="flex:2"><label style="font-size:11px;color:var(--muted-2);display:block;margin-bottom:2px">Git \u94fe\u63a5</label><input id="proj-git" type="text" placeholder="git@github.com:team/mall-system.git" style="width:100%;padding:5px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:12px"></div></div>'
    h += '<div style="margin-bottom:8px"><label style="font-size:11px;color:var(--muted-2);display:block;margin-bottom:2px">\u63cf\u8ff0</label><input id="proj-desc" type="text" placeholder="\u7535\u5546\u7cfb\u7edf" style="width:100%;padding:5px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:12px"></div>'
    h += '<div class="cfg-progress" id="proj-add-bar" style="display:none"><div class="cfg-progress__bar"></div><span>\u6b63\u5728\u9a8c\u8bc1 git \u94fe\u63a5\u5e76\u62c9\u53d6\u4ee3\u7801...</span></div>'
    h += '<button class="btn primary" onclick="addProject()" style="padding:6px 16px;border:none;border-radius:4px;background:var(--accent);color:#000;font-size:12px;cursor:pointer">\u6dfb\u52a0\u9879\u76ee</button></div>'
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
  h += '<button class="btn" onclick="saveKeyStrategy()" style="padding:5px 12px;border:1px solid var(--border);border-radius:4px;background:var(--bg-2);color:var(--fg);font-size:12px;cursor:pointer">\u5e94\u7528</button></div></div>'
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
  h += '<button class="btn primary" onclick="addKey()" style="padding:6px 16px;border:none;border-radius:4px;background:var(--accent);color:#000;font-size:12px;cursor:pointer">\u6dfb\u52a0\u5bc6\u94a5</button></div>'
  h += '</div>'
  contentHtml.value = h
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
      h += '<div class="card-main"><div class="card-title"><span class="name"><code>' + escH(s.name) + '</code></span>' + srcBadge + '</div>'
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
    h += '<button class="btn primary" onclick="addMCPServer()" style="padding:6px 16px;border:none;border-radius:4px;background:var(--accent);color:#000;font-size:12px;cursor:pointer">\u6dfb\u52a0\u670d\u52a1\u5668</button></div>'
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
      h += '<div class="card-main"><div class="card-title"><span class="name">' + escH(s.name) + '</span>' + scopeBadge + '</div>'
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
    h += '<div style="margin-bottom:8px"><label style="font-size:11px;color:var(--muted-2);display:block;margin-bottom:2px">\u5185\u5bb9</label><textarea id="skill-body" style="min-height:100px;width:100%;padding:6px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:12px;font-family:var(--mono)" placeholder="\u64cd\u4f5c\u6307\u5357 markdown..."></textarea></div>'
    h += '<button class="btn primary" onclick="addSkill()" style="padding:6px 16px;border:none;border-radius:4px;background:var(--accent);color:#000;font-size:12px;cursor:pointer">\u4fdd\u5b58 Skill</button></div>'
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
      h += '<span style="margin-left:6px;font-size:10px;padding:1px 6px;border-radius:99px;background:rgba(76,175,80,.15);color:#4caf50">\u5168\u5c40</span></div>'
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
      h += '<span class="badge" style="margin-left:6px;font-size:10px;padding:1px 6px;border-radius:99px;background:var(--accent-soft);color:var(--accent)">' + typeLabel + '</span></div>'
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
    h += '<div style="margin-bottom:8px"><label style="font-size:11px;color:var(--muted-2);display:block;margin-bottom:2px">\u5185\u5bb9</label><textarea id="mem-body" style="min-height:100px;width:100%;padding:6px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:12px;font-family:var(--mono)" placeholder="Markdown \u683c\u5f0f..."></textarea></div>'
    h += '<button class="btn primary" onclick="addMemory()" style="padding:6px 16px;border:none;border-radius:4px;background:var(--accent);color:#000;font-size:12px;cursor:pointer">\u4fdd\u5b58\u8bb0\u5fc6</button></div>'
  } catch (e) {
    h += '<div style="color:#f44336;padding:12px">\u52a0\u8f7d\u5931\u8d25</div>'
  }
  h += '</div>'
  contentHtml.value = h
}

async function renderCapability(kind: string) {
  const q = tokenQuery()
  const labels: Record<string, string> = { mcp: "MCP \u670d\u52a1\u5668", soul: "AI \u4eba\u683c", skills: "Skills" }
  const descs: Record<string, string> = { mcp: "\u914d\u7f6e MCP \u670d\u52a1\u5668\u6765\u6269\u5c55 Agent \u7684\u5de5\u5177\u80fd\u529b\u3002", soul: "\u5b9a\u5236 AI \u7684\u7cfb\u7edf\u63d0\u793a\u8bcd\u548c\u884c\u4e3a\u98ce\u683c\u3002", skills: "\u7ba1\u7406\u9879\u76ee\u7ea7\u7684\u53ef\u590d\u7528\u811a\u672c\u548c\u81ea\u52a8\u5316\u6d41\u7a0b\u3002" }
  const icons: Record<string, string> = { mcp: "\ud83d\udd27", soul: "\ud83e\udde0", skills: "\ud83d\udcdc" }
  let h = '<div class="section"><h3>' + (icons[kind] || "") + " " + (labels[kind] || kind) + '</h3><p class="desc">' + (descs[kind] || "") + '</p></div>'
  try {
    const resp = await fetch("/teamix/capabilities" + q)
    const data = await resp.json()
    const cfg = data[kind] || {}
    h += '<div class="section"><div class="section-title">\u5f53\u524d\u914d\u7f6e</div>'
    h += '<div style="color:var(--muted-2);font-size:12px;margin-bottom:8px">\u4fdd\u5b58\u5230 .reasonix/capabilities/' + kind + '.yaml</div>'
    h += '<textarea id="' + kind + '-raw" style="min-height:180px;width:100%;padding:8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:12px;font-family:var(--mono)">' + escH(JSON.stringify(cfg, null, 2)) + '</textarea>'
    h += '<div style="margin-top:8px;display:flex;gap:8px"><button class="btn primary" onclick="saveCapability(\'' + kind + '\')" style="padding:6px 16px;border:none;border-radius:4px;background:var(--accent);color:#000;font-size:12px;cursor:pointer">\u4fdd\u5b58\u914d\u7f6e</button><button class="btn" onclick="switchSettingsTab(\'' + kind + '\')" style="padding:6px 16px;border:1px solid var(--border);border-radius:4px;background:var(--bg-2);color:var(--fg);font-size:12px;cursor:pointer">\u8fd8\u539f</button></div></div>'
  } catch (e) {
    h += '<div style="color:#f44336;padding:12px">\u52a0\u8f7d\u5931\u8d25</div>'
  }
  contentHtml.value = h
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
  if (!name || !cmd) return
  const t = localStorage.getItem("teamix_token")
  if (!t) return
  await fetch("/teamix/mcp/add?token=" + encodeURIComponent(t), {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name, command: cmd, transport, args, scope })
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
  const userBtn = target.closest("[data-user-del]") as HTMLElement | null
  if (userBtn) {
    ev.preventDefault()
    const name = userBtn.getAttribute("data-user-del")
    if (name) removeUser(name)
    return
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
  const body = (document.getElementById("mem-body") as HTMLTextAreaElement)?.value
  if (!name) return
  const t = localStorage.getItem("teamix_token")
  if (!t) return
  await fetch("/teamix/memory/save?token=" + encodeURIComponent(t), {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name, title, description: desc, type: mtype || "user", body: body || "", scope })
  })
  tab.value = "memory"
  await refreshTab("memory")
}
w.addUser = async function() {
  const name = (document.getElementById("user-name") as HTMLInputElement)?.value.trim()
  const role = (document.getElementById("user-role") as HTMLSelectElement)?.value || "developer"
  if (!name) return
  const t = localStorage.getItem("teamix_token")
  if (!t) return
  await fetch("/teamix/users/add?token=" + encodeURIComponent(t), {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name, role })
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
  const body = (document.getElementById("skill-body") as HTMLTextAreaElement)?.value
  if (!name) return
  const t = localStorage.getItem("teamix_token")
  if (!t) return
  await fetch("/teamix/skills/save?token=" + encodeURIComponent(t), {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name, description: desc, body: body || "", scope })
  })
  tab.value = "skills"
  await refreshTab("skills")
}
w.saveCapability = async function(kind: string) {
  const el = document.getElementById(kind + "-raw") as HTMLTextAreaElement
  if (!el) return
  const t = localStorage.getItem("teamix_token")
  if (!t) return
  await fetch("/teamix/capabilities/save?token=" + encodeURIComponent(t), {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ kind, data: el.value })
  })
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
