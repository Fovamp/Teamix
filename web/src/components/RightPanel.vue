<script setup lang="ts">
import { ref, watch, onMounted, onUnmounted } from "vue"
import { api } from "../api"

const emit = defineEmits<{ (e: "open-projects"): void }>()

const showRp = ref(true)
const treeData = ref<any>(null)
const notifications = ref<any[]>([])
const notiCollapsed = ref(true)
const projectName = ref("项目文件")
const currentProject = ref("")

onMounted(async () => {
  try {
    treeData.value = await api.tree()
    updateProjectName()
  } catch {}
  try {
    const st = await api.status()
    if (st && st.selectedProject) {
      currentProject.value = st.selectedProject
      projectName.value = st.selectedProject
    }
  } catch {}
  window.addEventListener("notifications-update", onNotiUpdate as any)
  window.addEventListener("teamix-project-selected", reloadTree as any)
  const saved = localStorage.getItem("rp_noti_collapsed")
  if (saved !== null) notiCollapsed.value = saved === "true"

  // rp-resize-h drag for tree/noti split
  const resize = document.getElementById('rp-resize-h')
  const tree = document.querySelector('.right-panel__tree') as HTMLElement
  const noti = document.querySelector('.rp-noti') as HTMLElement
  if (resize && tree && noti) {
    let startY = 0, startTreeFlex = 3, startNotiFlex = 2
    const onPDown = (ev: PointerEvent) => {
      ev.preventDefault()
      resize.setPointerCapture(ev.pointerId)
      resize.classList.add('rp-resize-h--dragging')
      startY = ev.clientY
      startTreeFlex = parseFloat(tree.style.flex) || 3
      startNotiFlex = parseFloat(noti.style.flex) || 2
    }
    const onPMove = (ev: PointerEvent) => {
      if (!resize.classList.contains('rp-resize-h--dragging')) return
      const panel = resize.parentElement!
      const ph = panel.clientHeight
      if (ph < 50) return
      const dy = ev.clientY - startY
      const total = startTreeFlex + startNotiFlex
      const fpp = total / ph
      const ntf = Math.max(1, startTreeFlex + dy * fpp)
      const nnf = Math.max(0.5, total - ntf)
      if (ntf + nnf < 1) return
      tree.style.flex = String(ntf)
      noti.style.flex = String(nnf)
    }
    const onPUp = () => { resize.classList.remove('rp-resize-h--dragging') }
    resize.addEventListener('pointerdown', onPDown)
    resize.addEventListener('pointermove', onPMove)
    resize.addEventListener('pointerup', onPUp)
  }
})

watch(treeData, () => { setTimeout(loadFileTree, 50) })

onUnmounted(() => {
  window.removeEventListener("notifications-update", onNotiUpdate as any)
  window.removeEventListener("teamix-project-selected", reloadTree as any)
})

async function reloadTree() {
  try {
    treeData.value = await api.tree()
    updateProjectName()
    const st = await api.status().catch(() => null)
    if (st && st.selectedProject) {
      currentProject.value = st.selectedProject
      projectName.value = st.selectedProject
    }
  } catch {}
}

function onNotiUpdate(e: CustomEvent) {
  if (Array.isArray(e.detail)) notifications.value = e.detail
}

function updateProjectName() {
  if (treeData.value && treeData.value[0]?.path) {
    const parts = treeData.value[0].path.replace(/\\\\/g, '/').split('/')
    projectName.value = parts.slice(-2).join('/')
  }
}

function toggleNoti() {
  notiCollapsed.value = !notiCollapsed.value
  localStorage.setItem("rp_noti_collapsed", String(notiCollapsed.value))
}

function notiBadgeClass() {
  const unread = notifications.value.filter((n: any) => !n.read).length
  return unread === 0 ? "rp-noti__badge rp-noti__badge--empty" : "rp-noti__badge"
}

function markRead(n: any) {
  if (!n.read && n.id) {
    n.read = true
    const t = localStorage.getItem("teamix_token")
    if (!t) return
    fetch("/teamix/notifications/read?token=" + encodeURIComponent(t), {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ id: n.id, project: n.project || currentProject.value || "" })
    }).catch(() => {})
  }
}

// ── File tree: recursive DOM approach matching original rn() + tog() ──
function loadFileTree() {
  const el = document.getElementById('rp-tree')
  if (!el || !treeData.value) return
  if (treeData.value.empty !== undefined) {
    if (treeData.value.reason === "noProject") {
      el.innerHTML = '<div style="padding:12px;color:var(--muted-2);font-size:12px;text-align:center">请先选择项目</div>'
    } else {
      el.innerHTML = '<div style="padding:12px;color:var(--muted-2);font-size:12px;text-align:center">项目目录为空</div>'
    }
    return
  }
  if (treeData.value.length === 0) {
    el.innerHTML = '<div style="padding:12px;color:var(--muted-2);font-size:12px;text-align:center">目录为空</div>'
    return
  }
  el.innerHTML = ''
  treeData.value.forEach((n: any) => { el.appendChild(rn(n, 0)) })
}

function rn(n: any, d: number): HTMLElement {
  const wrap = document.createElement('div')
  const _rp = n.path || n.name || ''
  wrap.setAttribute('data-p', _rp)
  if (n.isDir) {
    const header = document.createElement('div')
    header.className = 'rp-item rp-item--dir'
    const hasKids = n.children && n.children.length > 0
    const a = document.createElement('span')
    a.className = 'rp-a'
    a.textContent = hasKids ? '\u25b6' : ''
    header.appendChild(a)
    const l = document.createElement('span')
    l.className = 'rp-l'
    l.textContent = n.name
    header.appendChild(l)
    wrap.appendChild(header)
    wrap.draggable = true
    wrap.title = _rp
    wrap.ondragstart = (e) => {
      const el = (e.target as HTMLElement).closest('[data-p]')
      const p = el ? el.getAttribute('data-p') : _rp
      ;(window as any)._dragPath = '@' + p + '/'
      e.dataTransfer!.setData('text/plain', (window as any)._dragPath)
      e.dataTransfer!.effectAllowed = 'copy'
    }
    const c = document.createElement('div')
    c.className = 'rp-c'
    c.style.display = 'none'
    wrap.appendChild(c)
    if (hasKids) {
      n.children.forEach((ch: any) => { c.appendChild(rn(ch, d + 1)) })
      function tog() {
        if (a.textContent === '\u25bc') {
          a.textContent = '\u25b6'
          c.style.display = 'none'
        } else {
          a.textContent = '\u25bc'
          c.style.display = ''
          const kids = c.querySelectorAll('.rp-c')
          for (let i = 0; i < kids.length; i++) { (kids[i] as HTMLElement).style.display = 'none' }
          const arr = c.querySelectorAll('.rp-a')
          for (let i = 0; i < arr.length; i++) { if (arr[i].textContent === '\u25bc') arr[i].textContent = '\u25b6' }
        }
      }
      a.onclick = (e) => { e.stopPropagation(); tog() }
      header.onclick = () => { tog() }
    }
  } else {
    wrap.className = 'rp-item'
    wrap.draggable = true
    wrap.title = _rp
    wrap.ondragstart = (e) => {
      const el = (e.target as HTMLElement).closest('[data-p]')
      const p = el ? el.getAttribute('data-p') : _rp
      ;(window as any)._dragPath = '@' + p
      e.dataTransfer!.setData('text/plain', (window as any)._dragPath)
      e.dataTransfer!.effectAllowed = 'copy'
    }
    const s = document.createElement('span')
    s.style.cssText = 'width:14px;display:inline-block;flex-shrink:0'
    wrap.appendChild(s)
    const l = document.createElement('span')
    l.className = 'rp-l'
    l.textContent = n.name
    wrap.appendChild(l)
    wrap.onclick = () => { openFilePreview(_rp) }
  }
  return wrap
}

function openFilePreview(path: string) {
  if (!path) return
  let p = document.getElementById('preview-panel')
  if (!p) {
    p = document.createElement('div')
    p.id = 'preview-panel'
    p.style.cssText = 'position:fixed;right:0;top:0;bottom:0;width:400px;background:var(--panel);border-left:2px solid var(--accent);z-index:50;display:flex;flex-direction:column;box-shadow:var(--shadow-lg);animation:msg-in .2s ease;min-width:200px'
    p.innerHTML = '<div id="pv-resize" style="position:absolute;left:-4px;top:0;bottom:0;width:8px;cursor:col-resize;z-index:5"></div><div style="display:flex;align-items:center;justify-content:space-between;padding:8px 12px;border-bottom:1px solid var(--border);font-size:13px;font-weight:500"><span id="pv-title"></span><span id="pv-close" style="cursor:pointer;font-size:18px;color:var(--muted-2);line-height:1">&times;</span></div><pre id="pv-body" style="flex:1;overflow:auto;padding:12px;font-family:var(--mono);font-size:12px;line-height:1.5;color:var(--fg-2);white-space:pre-wrap;margin:0"></pre>'
    document.body.appendChild(p)
    document.getElementById('pv-close')!.onclick = () => { p!.style.display = 'none' }
    const rv = document.getElementById('pv-resize')!
    let startX = 0, startW = 0
    rv.addEventListener('mousedown', function(e) {
      startX = e.clientX; startW = p!.offsetWidth
      document.body.style.cursor = 'col-resize'; document.body.style.userSelect = 'none'
      function mm(ev: MouseEvent) { const w = Math.max(200, Math.min(800, startW + startX - ev.clientX)); p!.style.width = w + 'px' }
      function mu() { document.body.style.cursor = ''; document.body.style.userSelect = ''; document.removeEventListener('mousemove', mm); document.removeEventListener('mouseup', mu) }
      document.addEventListener('mousemove', mm)
      document.addEventListener('mouseup', mu)
      e.preventDefault()
    })
  }
  p.style.display = 'flex'
  document.getElementById('pv-title')!.textContent = path
  document.getElementById('pv-body')!.textContent = 'Loading...'
  const token = localStorage.getItem('teamix_token')
  const url = '/teamix/file?path=' + encodeURIComponent(path) + (token ? '&token=' + encodeURIComponent(token) : '')
  fetch(url)
    .then(r => r.json())
    .then(data => { document.getElementById('pv-body')!.textContent = data.body || 'Empty file' })
    .catch(() => { document.getElementById('pv-body')!.textContent = 'Error loading file' })
}
</script>

<template>
  <aside class="right-panel" v-if="showRp" id="right-panel">
    <div class="right-panel__title" id="rp-project-name" style="cursor:pointer" title="点击选择项目" @click="emit('open-projects')">
      <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="vertical-align:-1px;margin-right:6px"><path d="M3 7v10l7 4V11z"/><path d="M3 7l7-4 7 4-7 4z"/><path d="M17 7v10"/><path d="M10 11v10"/><path d="M17 11h4v6h-4"/></svg>
      {{ currentProject || "选择项目" }} <span style="color:var(--muted-2);font-size:10px;margin-left:4px">切换</span>
    </div>
    <div class="right-panel__tree" id="rp-tree" style="flex:3;min-height:80px;padding:4px 0;overflow-y:auto;font-size:12px">
      <div v-if="!treeData" style="padding:16px;font-size:12px;color:var(--muted-2)">加载文件树...</div>
    </div>
    <div class="rp-resize-h" id="rp-resize-h"></div>
    <div class="rp-noti" id="rp-noti" :class="{ 'rp-noti--collapsed': notiCollapsed }" style="flex:2;min-height:60px">
      <div class="rp-noti__head" id="rp-noti-head" @click="toggleNoti">
        <svg class="rp-noti__head-arrow" width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="6 9 12 15 18 9"/></svg>
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9"/><path d="M13.73 21a2 2 0 0 1-3.46 0"/></svg>
        通知
        <span :class="notiBadgeClass()" id="rp-noti-badge">{{ notifications.filter((n: any) => !n.read).length || '0' }}</span>
      </div>
      <div class="rp-noti__list" id="rp-noti-list" :class="{ 'rp-noti__list--open': !notiCollapsed }">
        <div v-if="notifications.length === 0" class="rp-noti__empty">暂无通知</div>
        <div v-for="(n, i) in notifications" :key="n.id || i"
          class="rp-noti__item" :class="{ 'rp-noti__item--read': n.read }"
          @click="markRead(n)">
          <div class="rp-noti__from">
            <span class="dot"></span>
            {{ n.fromUser || '系统' }}
            <span class="rp-noti__time">{{ new Date(n.time || n.createdAt).toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }) }}</span>
          </div>
          <div class="rp-noti__msg">{{ n.message }}</div>
          <span v-if="n.fileChanged" class="rp-noti__file">{{ n.fileChanged }}</span>
        </div>
      </div>
    </div>
  </aside>
</template>

<style scoped>
.rp-item { padding-top: 2px; padding-bottom: 2px; cursor: default; display: flex; gap: 4px; align-items: center; }
.rp-item:hover { background: var(--card-hover); }
.rp-item--dir { cursor: pointer; }
.rp-a { width: 14px; flex-shrink: 0; cursor: pointer; font-size: 10px; color: var(--muted-2); }
.rp-l { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
</style>
