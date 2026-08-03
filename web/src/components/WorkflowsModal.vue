<script setup lang="ts">
import { ref, watch, onMounted } from "vue"
import { api } from "../api"
const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{ (e: "close"): void }>()
const templates = ref<any[]>([])
const showConfirm = ref(false)
const confirmMsg = ref('')
const currentDeleteName = ref('')
const currentDeleteScope = ref('private')
const loading = ref(false)
const isArchitect = ref(false)

// Editor state
const showEditor = ref(false)
const editTitle = ref("新增工作流")
const editName = ref("")
const editLabel = ref("")
const editDesc = ref("")
const editStages = ref<any[]>([])
const editScope = ref("private")
const editErr = ref("")
const showFse = ref(false)
const fseText = ref('')
const fseTarget = ref<{ set: (v: string) => void } | null>(null)

onMounted(async () => {
  try { const r = await api.userRole(); isArchitect.value = r.role === 'architect' } catch {}
})

watch(() => props.visible, async (v) => {
  if (v) {
    loading.value = true
    try {
      const ts = await api.workflowTemplates()
      templates.value = Array.isArray(ts) ? ts : []
    } catch { templates.value = [] }
    loading.value = false
  }
})

async function selectWf(name: string) {
  if (!name) return
  try { await api.workflowSelect(name) } catch {}
  emit("close")
  if (name === 'none') {
    localStorage.removeItem('teamix_wf_name')
    window.dispatchEvent(new CustomEvent("workflow-changed"))
    window.dispatchEvent(new CustomEvent("workflow-selected", { detail: '' }))
    return
  }
  const tpl = templates.value.find((t: any) => t.name === name)
  const label = tpl?.label || tpl?.name || name
  window.dispatchEvent(new CustomEvent("workflow-changed"))
  window.dispatchEvent(new CustomEvent("workflow-selected", { detail: label }))
}

function showDeleteConfirm(name: string) {
  if (!name || name === 'none') return
  const tpl = templates.value.find((t: any) => t.name === name)
  const label = tpl?.label || name
  currentDeleteName.value = name
  currentDeleteScope.value = tpl?.source === 'global' ? 'global' : 'private'
  confirmMsg.value = '确定删除工作流\u201c' + label + '\u201d吗？'
  showConfirm.value = true
}

async function deleteWf(name: string, scope: string) {
  if (!name || name === 'none') return
  try {
    await fetch("/teamix/workflows/template/delete?token=" + encodeURIComponent(localStorage.getItem("teamix_token") || ""), {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name, scope })
    })
    const ts = await api.workflowTemplates()
    templates.value = Array.isArray(ts) ? ts : []
  } catch {}
}

function openEditor(name?: string) {
  editErr.value = ""
  if (name) {
    const tpl = templates.value.find((t: any) => t.name === name)
    editScope.value = tpl?.source === 'global' ? 'global' : 'private'
    editTitle.value = "编辑工作流"
    showEditor.value = true
    fetch('/teamix/workflows/template?name=' + encodeURIComponent(name) + '&scope=' + editScope.value + '&token=' + encodeURIComponent(localStorage.getItem("teamix_token") || ''))
      .then(r => r.json()).then((t: any) => {
        editName.value = name || ''
        editLabel.value = t.label || ''
        editDesc.value = t.description || ''
        editStages.value = (t.stages || []).map((s: any, i: number) => ({
          id: 'stage-' + i + '-' + (Date.now() + Math.random()),
          sname: s.name || '',
          label: s.label || '',
          prompt: s.prompt || ''
        }))
      }).catch(() => {})
  } else {
    editTitle.value = "新增工作流"
    editName.value = ''
    editLabel.value = ''
    editDesc.value = ''
    editStages.value = []
    editScope.value = isArchitect.value ? 'global' : 'private'
    showEditor.value = true
  }
}

function closeEditor() { showEditor.value = false }

function addStage() {
  editStages.value.push({
    id: 'stage-' + (Date.now() + Math.random()),
    sname: '', label: '', prompt: ''
  })
}

function removeStage(idx: number) {
  editStages.value.splice(idx, 1)
  updateDesc()
}

function moveStage(idx: number, dir: number) {
  const target = idx + dir
  if (target < 0 || target >= editStages.value.length) return
  const arr = editStages.value
  const tmp = arr[idx]
  arr[idx] = arr[target]
  arr[target] = tmp
  updateDesc()
}

function updateDesc() {
  const labels = editStages.value.map((s: any) => s.label).filter(Boolean)
  if (labels.length > 0) editDesc.value = labels.join(' → ')
}

async function saveWorkflow() {
  const label = editLabel.value.trim()
  if (!label) { editErr.value = '请输入工作流名称'; return }
  let name = editName.value
  if (!name) {
    name = label.toLowerCase().replace(/[^a-z0-9-]/g, '').replace(/-+/g, '-').replace(/^-/, '').replace(/-$/, '')
    if (!name) name = 'workflow-' + Date.now()
  }
  const stages = editStages.value
    .filter((s: any) => s.sname.trim())
    .map((s: any) => ({
      name: s.sname.trim(),
      label: s.label.trim() || s.sname.trim(),
      prompt: s.prompt || ''
    }))
  const payload = {
    name,
    label,
    description: editDesc.value.trim(),
    stages,
    scope: editScope.value
  }
  const t = localStorage.getItem('teamix_token')
  if (!t) return
  try {
    const resp = await fetch('/teamix/workflows/template/save?token=' + encodeURIComponent(t), {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    })
    const data = await resp.json()
    if (data && data.ok) {
      closeEditor()
      const ts = await api.workflowTemplates()
      templates.value = Array.isArray(ts) ? ts : []
    } else {
      editErr.value = data?.error || '保存失败'
    }
  } catch (e) {
    editErr.value = '保存失败: ' + String(e)
  }
}

function openFse(s: any) {
  fseText.value = s.prompt
  fseTarget.value = { set: (v: string) => { s.prompt = v } }
  showFse.value = true
}

function saveFse() {
  if (fseTarget.value) fseTarget.value.set(fseText.value)
  showFse.value = false
}

function closeFse() { showFse.value = false }

const dragIdx = ref(-1)
const containerRef = ref<HTMLElement | null>(null)
function onDragStart(e: DragEvent, idx: number) {
  // Only allow drag from the handle, not from inputs
  const handle = (e.target as HTMLElement).closest('.we-stage-drag-handle')
  if (!handle) { e.preventDefault(); return }
  dragIdx.value = idx
  e.dataTransfer!.effectAllowed = 'move'
  e.dataTransfer!.setData('text/plain', '')
  const row = (e.target as HTMLElement).closest('.stage-row') as HTMLElement
  if (row) row.classList.add('dragging')
}
function onDragOver(e: DragEvent, idx: number) {
  e.preventDefault()
  e.dataTransfer!.dropEffect = 'move'
  if (dragIdx.value < 0 || dragIdx.value === idx) return
  const container = containerRef.value
  if (!container) return
  container.querySelectorAll('.stage-row').forEach(r => r.classList.remove('drag-before'))
  const row = (e.target as HTMLElement).closest('.stage-row') as HTMLElement
  if (!row) return
  const after = e.offsetY > row.offsetHeight / 2
  const target = after ? row.nextElementSibling : row
  if (target && target.classList.contains('stage-row')) target.classList.add('drag-before')
}
function onDragLeave(e: DragEvent) {
  const row = (e.target as HTMLElement).closest('.stage-row')
  if (row) row.classList.remove('drag-before')
}
function onDrop(e: DragEvent, idx: number) {
  e.preventDefault()
  const from = dragIdx.value
  if (from < 0 || from === idx) {
    cleanupDrag(); return
  }
  const arr = editStages.value
  const item = arr.splice(from, 1)[0]
  const after = e.offsetY > (e.target as HTMLElement).closest('.stage-row')?.offsetHeight! / 2
  const insertAt = after ? idx + (from < idx ? 0 : 1) : idx
  arr.splice(insertAt, 0, item)
  cleanupDrag()
  updateDesc()
}
function onDragEnd() { cleanupDrag() }
function onTailDragOver(e: DragEvent) {
  if (dragIdx.value < 0) return
  e.dataTransfer!.dropEffect = 'move'
  const container = containerRef.value
  if (container) container.querySelectorAll('.stage-row').forEach(r => r.classList.remove('drag-before'))
  const tail = (e.target as HTMLElement).closest('.stage-tail') as HTMLElement
  if (tail) tail.style.borderTopColor = 'var(--accent)'
}
function onTailDragLeave(e: DragEvent) {
  const tail = (e.target as HTMLElement).closest('.stage-tail') as HTMLElement
  if (tail) tail.style.borderTopColor = 'transparent'
}
function onTailDrop(e: DragEvent) {
  const from = dragIdx.value
  if (from < 0) { cleanupDrag(); return }
  const arr = editStages.value
  const item = arr.splice(from, 1)[0]
  arr.push(item)
  cleanupDrag()
  updateDesc()
}
function cleanupDrag() {
  dragIdx.value = -1
  const container = containerRef.value
  if (container) {
    container.querySelectorAll('.stage-row').forEach(r => r.classList.remove('drag-before', 'dragging'))
  }
}
</script>

<template>
  <div class="modal-overlay" v-if="visible && !showEditor" @click.self="emit('close')" style="z-index:200">
    <div class="modal" style="width:min(500px,90vw)">
      <div class="modal__head">
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>
        <span>工作流</span>
        <span class="modal__close" @click="emit('close')">&times;</span>
      </div>
      <div class="model-list" style="padding:8px;max-height:50vh;overflow-y:auto">
        <div v-if="loading" style="color:var(--muted-2);text-align:center;padding:20px;font-size:13px">加载中...</div>
        <div v-else-if="templates.length === 0" style="color:var(--muted-2);text-align:center;padding:20px;font-size:13px">暂无工作流模板</div>
        <div v-for="t in templates" :key="t.name" class="model-item" @click="selectWf(t.name)" style="cursor:pointer">
          <div style="min-width:0">
            <div class="model-item__title" :style="t.name === 'none' ? { color: 'var(--muted)' } : {}">{{ t.name === 'none' ? '自由对话' : (t.label || t.name) }}<span v-if="t.source" class="wf-source-tag" :class="'wf-source--' + t.source">{{ t.source === 'global' ? '全局' : t.source === 'private' ? '私有' : t.source }}</span></div>
            <div class="model-item__meta" :title="t.name === 'none' ? '灵活模式，自由对话' : (t.description || '')">{{ t.name === 'none' ? '灵活模式，自由对话' : (t.description || '') }}</div>
            <button v-if="(t.source === 'private' || isArchitect) && t.name && t.name !== 'none'" class="branch-item__btn" style="color:var(--danger);margin-top:6px;width:80%" @click.stop="showDeleteConfirm(t.name)">删除工作流</button>
          </div>
          <div style="display:flex;flex-direction:column;gap:4px;align-items:stretch">
            <button v-if="(t.source === 'private' || isArchitect) && t.name && t.name !== 'none'" class="branch-item__btn" @click.stop="openEditor(t.name)">编辑</button>
            
            <button class="branch-item__btn" @click.stop="selectWf(t.name)">选择</button>
          </div>
        </div>
        <div v-if="isArchitect" style="margin-top:8px;text-align:center">
          <button class="branch-item__btn wf-new-btn" @click="openEditor()" style="width:100%;padding:7px 0;border:1px dashed var(--accent);border-radius:6px;background:transparent;color:var(--accent);font-size:12px;cursor:pointer;display:flex;align-items:center;justify-content:center">＋ 新增工作流</button>
        </div>
      </div>
    </div>
  </div>

  <!-- Editor Modal -->
  <div class="modal-overlay" v-if="showEditor" @click.self="closeEditor" style="z-index:201">
    <div class="modal" style="width:min(600px,85vw)">
      <div class="modal__head">
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/></svg>
        <span>{{ editTitle }}</span>
        <span class="modal__close" @click="closeEditor">&times;</span>
      </div>
      <div style="padding:8px 12px;display:flex;flex-direction:column;gap:8px;max-height:70vh;overflow-y:auto">
        <div v-if="editErr" style="color:var(--danger);font-size:12px;background:var(--danger-soft);padding:6px 10px;border-radius:4px">{{ editErr }}</div>
        <div style="display:flex;gap:8px">
          <div style="flex:1"><label style="font-size:11px;color:var(--muted-2)">名称</label><input v-model="editLabel" style="width:100%;padding:5px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:13px"></div>
          <div style="flex:1"><label style="font-size:11px;color:var(--muted-2)">范围</label><select v-if="isArchitect" v-model="editScope" style="width:100%;padding:5px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:13px"><option value="global">全局（全员可用）</option><option value="private">私有（仅自己）</option></select><div v-else style="width:100%;padding:5px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg-2);color:var(--muted-2);font-size:13px">私有（仅自己）</div></div>
          <div style="flex:2"><label style="font-size:11px;color:var(--muted-2)">描述</label><input v-model="editDesc" style="width:100%;padding:5px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:13px"></div>
        </div>
        <div><label style="font-size:11px;color:var(--muted-2)">阶段</label></div>
        <div ref="containerRef" style="display:flex;flex-direction:column;gap:4px;position:relative">
          <div v-for="(s, i) in editStages" :key="s.id" class="stage-row" @dragover="onDragOver($event, i)" @dragleave="onDragLeave($event)" @drop="onDrop($event, i)" @dragend="onDragEnd()" style="display:flex;gap:4px;align-items:center;padding:4px 6px;border:1px solid var(--border);border-radius:4px;background:var(--bg-2)">
            <span draggable="true" class="we-stage-drag-handle" @dragstart="onDragStart($event, i)" style="cursor:grab;color:var(--muted-2);font-size:14px;user-select:none">☰</span>
            <input v-model="s.sname" placeholder="ID" style="width:80px;padding:3px 4px;border:1px solid var(--border);border-radius:3px;background:var(--bg);color:var(--fg);font-size:11px;font-family:var(--mono)">
            <input v-model="s.label" placeholder="标签" style="width:80px;padding:3px 4px;border:1px solid var(--border);border-radius:3px;background:var(--bg);color:var(--fg);font-size:11px" @input="updateDesc">
            <textarea v-model="s.prompt" placeholder="提示词" style="flex:1;padding:3px 4px;border:1px solid var(--border);border-radius:3px;background:var(--bg);color:var(--fg);font-size:11px;height:28px;resize:vertical"></textarea>
            <button title="全屏编辑" class="fse-btn" style="width:20px;height:20px;border:none;border-radius:3px;background:transparent;color:var(--accent);cursor:pointer;font-size:14px;flex-shrink:0" @click="openFse(s)">↑</button>
            <button class="stage-del-btn" style="width:20px;height:20px;border:none;border-radius:3px;background:transparent;color:var(--danger);cursor:pointer;font-size:14px" @click="removeStage(i)">×</button>
          </div>
        </div>
        <div class="stage-tail" @dragover.prevent="onTailDragOver($event)" @dragleave="onTailDragLeave($event)" @drop.prevent="onTailDrop($event)" style="height:6px;transition:border-top .15s;border-top:2px solid transparent"></div>
        <button @click="addStage" class="add-stage-btn" style="width:100%;padding:7px 0;border:1px dashed var(--accent);border-radius:6px;background:transparent;color:var(--accent);font-size:12px;cursor:pointer">＋ 新增阶段</button>
      </div>
      <div style="display:flex;gap:8px;justify-content:flex-end;padding:8px 12px;border-top:1px solid var(--border)">
        <button @click="closeEditor" class="editor-btn-cancel" style="padding:6px 16px;border:1px solid var(--border);border-radius:6px;background:var(--bg-2);color:var(--fg-2);font-size:12px;cursor:pointer">取消</button>
        <button @click="saveWorkflow" class="editor-btn-save" style="padding:6px 16px;border:none;border-radius:6px;background:var(--accent);color:oklch(99% 0 0);font-size:12px;cursor:pointer">保存</button>
      </div>
    </div>
  </div>

    <!-- Fullscreen Prompt Editor -->
  <div class="modal-overlay" v-if="showFse" @click.self="closeFse" style="z-index:202">
    <div class="modal" style="width:min(700px,90vw)">
      <div class="modal__head">
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/></svg>
        <span>编辑提示词</span>
        <span class="modal__close" @click="closeFse">&times;</span>
      </div>
      <div style="padding:12px">
        <textarea v-model="fseText" style="width:100%;height:400px;padding:10px;border:1px solid var(--border);border-radius:6px;background:var(--bg);color:var(--fg);font-size:13px;font-family:var(--mono);resize:vertical;box-sizing:border-box"></textarea>
      </div>
      <div style="display:flex;gap:8px;justify-content:flex-end;padding:8px 12px;border-top:1px solid var(--border)">
        <button @click="closeFse" style="padding:6px 16px;border:1px solid var(--border);border-radius:6px;background:var(--bg-2);color:var(--fg-2);font-size:12px;cursor:pointer">取消</button>
        <button @click="saveFse" style="padding:6px 16px;border:none;border-radius:6px;background:var(--accent);color:oklch(99% 0 0);font-size:12px;cursor:pointer">确定保存</button>
      </div>
    </div>
  </div>

  <!-- Confirm Modal -->
  <div class="modal-overlay" v-if="showConfirm" @click.self="showConfirm = false" style="z-index:300">
    <div class="confirm-modal" style="background:var(--panel);border:1px solid var(--border-strong);border-radius:var(--radius-lg);width:min(380px,85vw);text-align:center;box-shadow:var(--shadow-lg)">
      <div class="confirm-modal__head" style="padding:14px 16px 0;font-size:14px;font-weight:500;color:var(--fg)">{{ confirmMsg }}</div>
      <div class="confirm-modal__actions" style="display:flex;gap:8px;justify-content:center;padding:16px">
        <button class="confirm-modal__btn confirm-modal__btn--cancel" @click="showConfirm = false" style="padding:7px 20px;border-radius:var(--radius);font-size:12px;cursor:pointer;border:1px solid var(--border);background:var(--bg-2);color:var(--fg-2)">取消</button>
        <button class="confirm-modal__btn confirm-modal__btn--ok" @click="showConfirm = false; deleteWf(currentDeleteName, currentDeleteScope)" style="padding:7px 20px;border-radius:var(--radius);font-size:12px;cursor:pointer;border:none;background:var(--accent);color:oklch(99% 0 0)">确定</button>
      </div>
    </div>
  </div>

</template>
<style scoped>
.model-item__meta {
  font-size: 11px;
  color: var(--muted);
  margin-top: 2px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.confirm-modal__btn { padding: 7px 20px; border-radius: var(--radius); font-size: 12px; cursor: pointer; }
.stage-row { cursor: grab; transition: border-color .15s, background .15s; }
.stage-row:active { cursor: grabbing; }
.stage-row.drag-before { border-top: 2px solid var(--accent) !important; border-radius: 0 !important; }
.stage-row.dragging { opacity: .4; }
.we-stage-drag-handle:hover { color: var(--accent) !important; }
.wf-source-tag { display:inline-block; margin-left:6px; padding:1px 6px; border-radius:99px; font-size:10px; font-weight:500; vertical-align:middle; } .wf-source--global { background:var(--accent-soft); color:var(--accent); } .wf-source--private { background:#e8f5e9; color:#2e7d32; }
.wf-new-btn:hover {
  border-color: var(--accent) !important;
  background: var(--accent-soft) !important;
  color: var(--accent) !important;
}
.branch-item__btn {
  height: 26px;
  padding: 0 9px;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--panel);
  color: var(--fg-2);
  font-size: 11.5px;
  cursor: pointer;
}
.confirm-modal__btn--ok:hover { opacity: 0.85 !important; }
.confirm-modal__btn--cancel:hover { border-color: var(--border-strong) !important; color: var(--fg) !important; }
.branch-item__btn:hover {
  border-color: var(--border-strong);
  color: var(--fg);
}
.editor-btn-cancel:hover {
  border-color: var(--border-strong) !important;
  color: var(--fg) !important;
}
.editor-btn-save:hover {
  opacity: 0.85 !important;
}
.stage-del-btn:hover {
  background: var(--danger-soft) !important;
  color: #fff !important;
}
.fse-btn:hover {
  background: var(--accent-soft) !important;
  color: var(--accent) !important;
}
.add-stage-btn:hover {
  border-color: var(--accent) !important;
  background: var(--accent-soft) !important;
  color: var(--accent) !important;
}
</style>

