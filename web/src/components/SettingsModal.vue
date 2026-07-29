<script setup lang="ts">
import { ref } from "vue"
import { api } from "../api"
const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{ (e: "close"): void }>()
const tab = ref("keys")
const tabs = ["keys", "mcp", "skills", "memory", "capabilities"]
const tabLbl: Record<string, string> = { keys: "API密钥", mcp: "MCP", skills: "Skills", memory: "记忆", capabilities: "Capabilities" }
</script>

<template>
  <div class="modal-overlay" v-if="visible" @click.self="emit('close')" style="z-index:200">
    <div class="modal" style="width:min(780px,90vw);height:65vh;display:flex;flex-direction:column">
      <div class="modal__head" style="flex-shrink:0">
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/></svg>
        <span>设置</span>
        <span class="modal__close" @click="emit('close')">&times;</span>
      </div>
      <div style="display:flex;flex:1;min-height:0;overflow:hidden">
        <div class="settings-tabs" style="width:140px;flex-shrink:0;border-right:1px solid var(--border);padding:8px">
          <div v-for="t in tabs" :key="t" class="settings-tab" :class="{ active: tab === t }" @click="tab = t" style="padding:6px 10px;border-radius:6px;cursor:pointer;font-size:13px;margin-bottom:2px">{{ tabLbl[t] }}</div>
        </div>
        <div style="flex:1;overflow-y:auto;padding:12px;font-size:13px;color:var(--muted)">
          <div v-if="tab === 'keys'">
            <h3 style="margin-bottom:8px">API密钥池</h3>
            <p>密钥池状态和策略配置</p>
          </div>
          <div v-if="tab === 'mcp'">
            <h3 style="margin-bottom:8px">MCP 服务器</h3>
            <p>MCP 服务器列表和管理</p>
          </div>
          <div v-if="tab === 'skills'">
            <h3 style="margin-bottom:8px">Skills</h3>
            <p>技能开关列表</p>
          </div>
          <div v-if="tab === 'memory'">
            <h3 style="margin-bottom:8px">记忆管理</h3>
            <p>已保存的记忆条目</p>
          </div>
          <div v-if="tab === 'capabilities'">
            <h3 style="margin-bottom:8px">Capabilities</h3>
            <p>能力配置管理</p>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
