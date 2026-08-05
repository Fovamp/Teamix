<script setup lang="ts">
import { ref, onMounted, onUnmounted } from "vue"
import { api } from "./api"
import { initGlobalInteraction } from "./lib/longpress"
import LoginOverlay from "./components/LoginOverlay.vue"
import SideBar from "./components/SideBar.vue"
import ChatArea from "./components/ChatArea.vue"
import RightPanel from "./components/RightPanel.vue"
import StatsModal from "./components/StatsModal.vue"
import BranchesModal from "./components/BranchesModal.vue"
import ModelsModal from "./components/ModelsModal.vue"
import WorkflowsModal from "./components/WorkflowsModal.vue"
import SettingsModal from "./components/SettingsModal.vue"
import ProjectModal from "./components/ProjectModal.vue"
import SummaryModal from "./components/SummaryModal.vue"
import ToastContainer from "./components/ToastContainer.vue"

const showLogin = ref(!api.isLoggedIn())
const showStats = ref(false)
const showBranches = ref(false)
const showModels = ref(false)
const showWorkflows = ref(false)
const showSettings = ref(false)
const showProject = ref(false)
const showSummaries = ref(false)

// 左右两侧拖拽竖线（resize-divider）：
// 直接在模板中渲染，随 .app 一起挂载，无登录时序问题；
// 用 CSS 变量 calc() 定位到两列交界，不受面板 overflow 裁剪。
let dragging: 'left' | 'right' | null = null
let startX = 0, startW = 0

function onDividerDown(e: MouseEvent, side: 'left' | 'right') {
  dragging = side
  startX = e.clientX
  const panel = document.querySelector(side === 'left' ? '.sidebar' : '.right-panel') as HTMLElement
  startW = panel ? panel.offsetWidth : (side === 'left' ? 200 : 220)
  document.body.style.cursor = 'col-resize'
  document.body.style.userSelect = 'none'
  const dv = document.getElementById(side === 'left' ? 'app-divider-left' : 'app-divider-right')
  if (dv) dv.classList.add('resize-divider--active')
  e.preventDefault()
}

function onProjectSelected() {
  // 通知 RightPanel 等组件刷新文件树
  window.dispatchEvent(new CustomEvent("teamix-project-selected"))
}

const onMove = (e: MouseEvent) => {
  if (!dragging) return
  const dx = e.clientX - startX
  const cssVar = dragging === 'left' ? '--sidebar-w' : '--right-w'
  const newW = Math.max(180, Math.min(800, dragging === 'left' ? startW + dx : startW - dx))
  document.documentElement.style.setProperty(cssVar, newW + 'px')
}

const onUp = () => {
  if (!dragging) return
  const dv = document.getElementById(dragging === 'left' ? 'app-divider-left' : 'app-divider-right')
  if (dv) dv.classList.remove('resize-divider--active')
  dragging = null
  document.body.style.cursor = ''
  document.body.style.userSelect = ''
}

onMounted(() => {
  initGlobalInteraction()
  document.addEventListener('mousemove', onMove)
  document.addEventListener('mouseup', onUp)
  // 消息按钮"总结"生成后自动打开总结面板
  window.addEventListener('open-summaries', openSummaries)
})

function openSummaries() { showSummaries.value = true }

onUnmounted(() => {
  document.removeEventListener('mousemove', onMove)
  document.removeEventListener('mouseup', onUp)
  window.removeEventListener('open-summaries', openSummaries)
})
</script>

<template>
  <LoginOverlay v-if="showLogin" @login="showLogin = false" />
  <div v-else class="app">
    <div class="resize-divider app-divider" id="app-divider-left" @mousedown="onDividerDown($event, 'left')"></div>
    <div class="resize-divider app-divider" id="app-divider-right" @mousedown="onDividerDown($event, 'right')"></div>

    <SideBar @stats="showStats = true" @branches="showBranches = true" @models="showModels = true" @workflows="showWorkflows = true" @settings="showSettings = true" @summaries="showSummaries = true" />
    <ChatArea />
    <RightPanel @open-projects="showProject = true" />
    <StatsModal :visible="showStats" @close="showStats = false" />
    <BranchesModal :visible="showBranches" @close="showBranches = false" />
    <ModelsModal :visible="showModels" @close="showModels = false" />
    <WorkflowsModal :visible="showWorkflows" @close="showWorkflows = false" />
    <SettingsModal :visible="showSettings" @close="showSettings = false" />
    <ProjectModal :visible="showProject" @close="showProject = false" @selected="onProjectSelected" />
    <SummaryModal :visible="showSummaries" @close="showSummaries = false" />
    <ToastContainer />
  </div>
</template>

<style>
.app { position: relative; display: grid; grid-template-columns: var(--sidebar-w, 200px) 1fr var(--right-w, 220px); grid-template-rows: auto 1fr auto; height: 100vh; }
.app-divider { z-index: 30; }
#app-divider-left { left: calc(var(--sidebar-w, 200px) - 2px); }
#app-divider-right { left: calc(100% - var(--right-w, 220px) - 2px); }
.sidebar { grid-column: 1; grid-row: 1 / 4; }
.transcript { grid-column: 2; grid-row: 2; }
.footer { grid-column: 2; grid-row: 3; position: relative; }
.right-panel { grid-column: 3; grid-row: 1 / 4; display: flex; flex-direction: column; background: var(--panel); border-left: 1px solid var(--border); }
.sidebar-overlay { display: none; position: fixed; inset: 0; background: oklch(0% 0 0/.5); z-index: 40; }
.sidebar-overlay--visible { display: block; }

</style>
