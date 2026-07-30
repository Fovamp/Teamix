<script setup lang="ts">
import { ref, onMounted, onUnmounted } from "vue"
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
const showMobileSidebar = ref(false)

// Resizable dividers
onMounted(() => {
  const sidebar = document.querySelector('.sidebar') as HTMLElement
  const rightPanel = document.querySelector('.right-panel') as HTMLElement
  if (!sidebar || !rightPanel) return

  let dragging: any = null, startX = 0, startW = 0

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

  document.addEventListener('mousemove', onMove)
  document.addEventListener('mouseup', onUp)

  // Cleanup on unmount
  onUnmounted(() => {
    document.removeEventListener('mousemove', onMove)
    document.removeEventListener('mouseup', onUp)
  })
})
</script>

<template>
  <LoginOverlay v-if="showLogin" @login="showLogin = false" />
  <div v-else class="app" :class="{ 'sidebar--open': showMobileSidebar }">
    <button id="menu-btn" @click="showMobileSidebar = !showMobileSidebar"
      class="mobile-menu-btn">&#9776;</button>
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
.mobile-menu-btn { display: none; position: fixed; top: 12px; left: 12px; z-index: 60; width: 36px; height: 36px; border-radius: 8px; background: var(--panel); border: 1px solid var(--border); color: var(--fg); align-items: center; justify-content: center; font-size: 18px; cursor: pointer; }
@media (max-width: 768px) {
  .sidebar { position: fixed; left: -280px; top: 0; bottom: 0; width: 260px; z-index: 50; transition: left .25s ease; }
  .sidebar--open .sidebar { left: 0; }
  .app .mobile-menu-btn { display: flex !important; }
  .right-panel { display: none !important; }
  .app { grid-template-columns: 1fr; }
}
</style>
