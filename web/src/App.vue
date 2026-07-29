<script setup lang="ts">
import { ref } from "vue"
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
</style>
