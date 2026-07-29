// API response types
export interface Session {
  name: string
  path: string
  title?: string
  turns?: number
  current?: boolean
}

export interface EventMessage {
  kind: string
  body?: string
  data?: any
}

export interface Status {
  label: string
  running: boolean
  plan: boolean
  autoApproveTools: boolean
  bypass: boolean
  toolApprovalMode: string
  goal: string
  goalStatus: string
  cwd: string
  used: number
  window: number
  user: string
}

export interface ModelEntry {
  ref: string
  provider: string
  model: string
  kind?: string
  active?: boolean
  default?: boolean
}

export interface StageJSON {
  stage: string
  label: string
  status: string
}

export interface WorkflowState {
  stages: StageJSON[]
  current: string
}

export interface Notification {
  id: string
  title: string
  message: string
  type: string
  createdAt: number
  read: boolean
}

export interface Memory {
  name: string
  title: string
  description: string
  type: string
  body: string
}

export interface SkillsEntry {
  name: string
  enabled: boolean
  scope: string
  description: string
}

export interface MCPServer {
  name: string
  transport: string
  tools: number
  toolList: { name: string; description: string }[]
  status: string
  error?: string
}

export interface ProjectInfo {
  workspaceRoot: string
  projectName: string
}
