<script setup lang="ts">
import { ref, watch, onMounted } from "vue"
import { api } from "../api"
const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{ (e: "close"): void }>()
const templates = ref<any[]>([])
const showConfirm = ref(false)
const confirmMsg = ref('')
const currentDeleteName = ref('')
const loading = ref(false)
const isArchitect = ref(false)

// Editor state
const showEditor = ref(false)
const editTitle = ref("新增工作流")
const editName = ref("")
const editLabel = ref("")
const editDesc = ref("")
const editStages = ref<any[]>([])

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
  confirmMsg.value = '确定删除工作流\u201c' + label + '\u201d吗？'
  showConfirm.value = true
}

async function deleteWf(name: string) {
  if (!name || name === 'none') return
  try {
    await fetch("/teamix/workflows/template/delete?token=" + encodeURIComponent(localStorage.getItem("teamix_token") || ""), {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name })
    })
    const ts = await api.workflowTemplates()
    templates.value = Array.isArray(ts) ? ts : []
  } catch {}
}

function openEditor(name?: string) {
  if (name) {
    editTitle.value = "编辑工作流"
    showEditor.value = true
    fetch('/teamix/workflows/template?name=' + encodeURIComponent(name) + '&token=' + encodeURIComponent(localStorage.getItem("teamix_token") || ''))
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
  if (!label) { alert('请输入工作流名称'); return }
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
    stages
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
      alert('保存失败')
    }
  } catch (e) {
    alert('保存失败: ' + String(e))
  }
}

function onDragStart(e: DragEvent, idx: number) {
  (e.target as HTMLElement).parentElement?.parentElement?.setAttribute('data-drag-idx', String(idx))
  e.dataTransfer?.setData('text/plain', '')
}
function onDragOver(e: DragEvent, idx: number) {
  e.preventDefault()
  if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'
}
function onDrop(e: DragEvent, idx: number) {
  e.preventDefault()
  const from = parseInt((e.target as HTMLElement).closest('[data-drag-idx]')?.getAttribute('data-drag-idx') || '-1')
  if (from >= 0 && from !== idx) {
    const arr = editStages.value
    const tmp = arr[from]
    arr[from] = arr[idx]
    arr[idx] = tmp
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
            <div class="model-item__title" :style="t.name === 'none' ? { color: 'var(--muted)' } : {}">{{ t.name === 'none' ? '自由对话' : (t.label || t.name) }}</div>
            <div class="model-item__meta" :title="t.name === 'none' ? '灵活模式，自由对话' : (t.description || '')">{{ t.name === 'none' ? '灵活模式，自由对话' : (t.description || '') }}</div>
            <button v-if="isArchitect && t.name && t.name !== 'none'" class="branch-item__btn" style="color:var(--danger);margin-top:6px;width:80%" @click.stop="showDeleteConfirm(t.name)">删除工作流</button>
          </div>
          <div style="display:flex;flex-direction:column;gap:4px;align-items:stretch">
            <button v-if="isArchitect && t.name && t.name !== 'none'" class="branch-item__btn" @click.stop="openEditor(t.name)">编辑</button>
            
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
        <div style="display:flex;gap:8px">
          <div style="flex:1"><label style="font-size:11px;color:var(--muted-2)">名称</label><input v-model="editLabel" style="width:100%;padding:5px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:13px"></div>
          <div style="flex:2"><label style="font-size:11px;color:var(--muted-2)">描述</label><input v-model="editDesc" style="width:100%;padding:5px 8px;border:1px solid var(--border);border-radius:4px;background:var(--bg);color:var(--fg);font-size:13px"></div>
        </div>
        <div><label style="font-size:11px;color:var(--muted-2)">阶段</label></div>
        <div style="display:flex;flex-direction:column;gap:4px">
          <div v-for="(s, i) in editStages" :key="s.id" draggable="true" @dragstart="onDragStart($event, i)" @dragover="onDragOver($event, i)" @drop="onDrop($event, i)" style="display:flex;gap:4px;align-items:center;padding:4px 6px;border:1px solid var(--border);border-radius:4px;background:var(--bg-2)">
            <span style="cursor:grab;color:var(--muted-2);font-size:14px;user-select:none">☰</span>
            <input v-model="s.sname" placeholder="ID" style="width:80px;padding:3px 4px;border:1px solid var(--border);border-radius:3px;background:var(--bg);color:var(--fg);font-size:11px;font-family:var(--mono)">
            <input v-model="s.label" placeholder="标签" style="width:80px;padding:3px 4px;border:1px solid var(--border);border-radius:3px;background:var(--bg);color:var(--fg);font-size:11px" @input="updateDesc">
            <textarea v-model="s.prompt" placeholder="提示词" style="flex:1;padding:3px 4px;border:1px solid var(--border);border-radius:3px;background:var(--bg);color:var(--fg);font-size:11px;height:28px;resize:vertical"></textarea>
            <button class="stage-del-btn" style="width:20px;height:20px;border:none;border-radius:3px;background:transparent;color:var(--danger);cursor:pointer;font-size:14px" @click="removeStage(i)">×</button>
          </div>
        </div>
        <button @click="addStage" style="padding:5px 0;border:1px dashed var(--border);border-radius:4px;background:transparent;color:var(--muted);font-size:11px;cursor:pointer">+ 新增阶段</button>
      </div>
      <div style="display:flex;gap:8px;justify-content:flex-end;padding:8px 12px;border-top:1px solid var(--border)">
        <button @click="closeEditor" class="editor-btn-cancel" style="padding:6px 16px;border:1px solid var(--border);border-radius:6px;background:var(--bg-2);color:var(--fg-2);font-size:12px;cursor:pointer">取消</button>
        <button @click="saveWorkflow" class="editor-btn-save" style="padding:6px 16px;border:none;border-radius:6px;background:var(--accent);color:oklch(99% 0 0);font-size:12px;cursor:pointer">保存</button>
      </div>
    </div>
  </div>

  <!-- Confirm Modal -->
  <div class="modal-overlay" v-if="showConfirm" @click.self="showConfirm = false" style="z-index:300">
    <div class="confirm-modal" style="background:var(--panel);border:1px solid var(--border-strong);border-radius:var(--radius-lg);width:min(380px,85vw);text-align:center;box-shadow:var(--shadow-lg)">
      <div class="confirm-modal__head" style="padding:14px 16px 0;font-size:14px;font-weight:500;color:var(--fg)">{{ confirmMsg }}</div>
      <div class="confirm-modal__actions" style="display:flex;gap:8px;justify-content:center;padding:16px">
        <button class="confirm-modal__btn confirm-modal__btn--cancel" @click="showConfirm = false" style="padding:7px 20px;border-radius:var(--radius);font-size:12px;cursor:pointer;border:1px solid var(--border);background:var(--bg-2);color:var(--fg-2)">取消</button>
        <button class="confirm-modal__btn confirm-modal__btn--ok" @click="showConfirm = false; deleteWf(currentDeleteName)" style="padding:7px 20px;border-radius:var(--radius);font-size:12px;cursor:pointer;border:none;background:var(--accent);color:oklch(99% 0 0)">确定</button>
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
</style>

