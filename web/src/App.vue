<script setup lang="ts">
import { ref, watch, nextTick, onUnmounted } from "vue"
import { api } from "./api"
import LoginOverlay from "./components/LoginOverlay.vue"
import SideBar from "./components/SideBar.vue"
import ChatArea from "./components/ChatArea.vue"
import RightPanel from "./components/RightPanel.vue"
import StatsModal from "./components/StatsModal.vue"
import BranchesModal from "./components/BranchesModal.vue"
import ModelsModal from "./components/ModelsModal.vue"
import WorkflowsModal from "./components/WorkflowsModal.vue"
import SettingsModal from "./components/SettingsModal.vue"

const showLogin = ref(!api.isLoggedIn())
const showStats = ref(false)
const showBranches = ref(false)
const showModels = ref(false)
const showWorkflows = ref(false)
const showSettings = ref(false)

// Resizable dividers — 登录成功（v-else 渲染 .app）后再初始化，
// 避免首次进入时 .sidebar/.right-panel 不存在导致拖拽条永不创建。
let dividersReady = false
let dragging: any = null, startX = 0, startW = 0
// 引用提升到组件作用域，便于 onUnmounted 按名移除
let dragMoveFn: ((e: MouseEvent) => void) | null = null
let dragUpFn: (() => void) | null = null

function initDividers() {
  if (dividersReady) return
  const sidebar = document.querySelector('.sidebar') as HTMLElement
  const rightPanel = document.querySelector('.right-panel') as HTMLElement
  if (!sidebar || !rightPanel) return

  function onStart(e: MouseEvent, panel: HTMLElement, cssVar: string) {
    dragging = { panel, cssVar }
    startX = e.clientX
    startW = panel.offsetWidth
    document.body.style.cursor = 'col-resize'
    document.body.style.userSelect = 'none'
    e.preventDefault()
  }

  // Sidebar resize handle
  const sd = document.createElement('div')
  sd.className = 'resize-divider'
  sd.style.cssText = 'position:absolute;right:-2px;top:0;bottom:0;width:4px;cursor:col-resize;z-index:10'
  sidebar.style.position = 'relative'
  sidebar.appendChild(sd)
  sd.addEventListener('mousedown', (e) => onStart(e, sidebar, '--sidebar-w'))

  // Right panel resize handle
  const rd = document.createElement('div')
  rd.className = 'resize-divider'
  rd.style.cssText = 'position:absolute;left:-2px;top:0;bottom:0;width:4px;cursor:col-resize;z-index:10'
  rightPanel.style.position = 'relative'
  rightPanel.appendChild(rd)
  rd.addEventListener('mousedown', (e) => onStart(e, rightPanel, '--right-w'))

  const onMove = (e: MouseEvent) => {
    if (!dragging) return
    const dx = e.clientX - startX
    const delta = dragging.cssVar === '--sidebar-w' ? dx : -dx
    const newW = Math.max(180, Math.min(800, startW + delta))
    document.documentElement.style.setProperty(dragging.cssVar, newW + 'px')
  }
  const onUp = () => {
    if (!dragging) return
    document.body.style.cursor = ''
    document.body.style.userSelect = ''
    dragging = null
  }
  dragMoveFn = onMove
  dragUpFn = onUp
  document.addEventListener('mousemove', onMove)
  document.addEventListener('mouseup', onUp)
  dividersReady = true
}

// 登录成功后（v-else 渲染 .app）初始化拖拽条。
// 用轮询重试 + MutationObserver 兜底，确保任何挂载时序下都能创建。
let dividerRetryTimer: any = null
let dividerObserver: MutationObserver | null = null

function tryInitDividers() {
  if (dividersReady) return
  initDividers()
  if (!dividersReady) {
    // 元素还没就绪，稍后重试（登录渲染 + 子组件挂载需要时间）
    clearTimeout(dividerRetryTimer)
    dividerRetryTimer = setTimeout(tryInitDividers, 200)
  } else {
    clearTimeout(dividerRetryTimer)
    if (dividerObserver) { dividerObserver.disconnect(); dividerObserver = null }
  }
}

watch(showLogin, (v) => {
  if (v) return
  // 观察 body 变化，.app 子树挂载完成即触发
  dividerObserver = new MutationObserver(() => tryInitDividers())
  dividerObserver.observe(document.body, { childList: true, subtree: true })
  nextTick(tryInitDividers)
}, { immediate: true })

onUnmounted(() => {
  if (dragMoveFn) document.removeEventListener('mousemove', dragMoveFn)
  if (dragUpFn) document.removeEventListener('mouseup', dragUpFn)
  clearTimeout(dividerRetryTimer)
  if (dividerObserver) dividerObserver.disconnect()
})
</script>

<template>
  <LoginOverlay v-if="showLogin" @login="showLogin = false" />
  <div v-else class="app">

    <SideBar @stats="showStats = true" @branches="showBranches = true" @models="showModels = true" @workflows="showWorkflows = true" @settings="showSettings = true" />
    <ChatArea />
    <RightPanel />
    <StatsModal :visible="showStats" @close="showStats = false" />
    <BranchesModal :visible="showBranches" @close="showBranches = false" />
    <ModelsModal :visible="showModels" @close="showModels = false" />
    <WorkflowsModal :visible="showWorkflows" @close="showWorkflows = false" />
    <SettingsModal :visible="showSettings" @close="showSettings = false" />
  </div>
</template>

<style>
.app { display: grid; grid-template-columns: var(--sidebar-w, 200px) 1fr var(--right-w, 220px); grid-template-rows: auto 1fr auto; height: 100vh; }
.sidebar { grid-column: 1; grid-row: 1 / 4; }
.transcript { grid-column: 2; grid-row: 2; }
.footer { grid-column: 2; grid-row: 3; }
.right-panel { grid-column: 3; grid-row: 1 / 4; display: flex; flex-direction: column; background: var(--panel); border-left: 1px solid var(--border); }
.sidebar-overlay { display: none; position: fixed; inset: 0; background: oklch(0% 0 0/.5); z-index: 40; }
.sidebar-overlay--visible { display: block; }

</style>
