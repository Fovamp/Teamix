<script setup lang="ts">
import { ref, onMounted } from "vue"
import { api } from "../api"

const showRp = ref(true)
const treeData = ref<any>(null)
const treeExpanded: Record<string, boolean> = {}

onMounted(async () => {
  try { treeData.value = await api.tree() } catch {}
})

function toggle(key: string) {
  treeExpanded[key] = !treeExpanded[key]
}

function flattenTree(nodes: any[], depth = 0): any[] {
  if (!nodes) return []
  const r: any[] = []
  for (const n of nodes) {
    const k = n.path || n.name
    if (!(k in treeExpanded)) treeExpanded[k] = depth < 1
    r.push({ ...n, _key: k, _depth: depth, _open: treeExpanded[k] })
    if (n.children?.length && treeExpanded[k]) r.push(...flattenTree(n.children, depth + 1))
  }
  return r
}
</script>

<template>
  <aside class="right-panel" v-if="showRp">
    <div class="right-panel__title">{{ treeData?.[0]?.path?.split("/")[0] || "项目文件" }}</div>
    <div class="right-panel__tree" style="flex:3;min-height:80px;padding:4px 0;overflow-y:auto;font-size:12px">
      <div v-for="c in flattenTree(treeData)" :key="c._key"
        :style="{ paddingLeft: (8 + (c._depth || 0) * 16) + 'px' }"
        class="rp-item" :class="{ 'rp-item--dir': c.isDir }"
        :title="c.path"
        draggable="true"
        @dragstart="(e: any) => { window._dragPath = c.isDir ? '@' + c.path + '/' : '@' + c.path; e.dataTransfer.setData('text/plain', window._dragPath) }">
        <span v-if="c.isDir" class="rp-a" @click.stop="toggle(c._key)">{{ c._open ? "▼" : "▶" }}</span>
        <span v-else style="width:14px;display:inline-block;flex-shrink:0"></span>
        <span class="rp-l">{{ c.name }}</span>
      </div>
      <div v-if="!treeData" style="padding:16px;font-size:12px;color:var(--muted-2)">加载文件树...</div>
    </div>
    <div class="rp-resize-h"></div>
    <div class="rp-noti" style="flex:2;min-height:60px">
      <div class="rp-noti__head">
        <svg class="rp-noti__head-arrow" width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="6 9 12 15 18 9"/></svg>
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9"/><path d="M13.73 21a2 2 0 0 1-3.46 0"/></svg>
        通知
        <span class="rp-noti__badge rp-noti__badge--empty" id="rp-noti-badge">0</span>
      </div>
      <div class="rp-noti__list"></div>
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
