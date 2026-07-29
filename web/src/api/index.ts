// API layer — wraps all backend calls
const TOKEN_KEY = "teamix_token"
const USER_KEY = "teamix_user"

function token(): string {
  return localStorage.getItem(TOKEN_KEY) || ""
}

function authQuery(): string {
  const t = token()
  return t ? "?token=" + encodeURIComponent(t) : ""
}

function authHeaders(): Record<string, string> {
  const t = token()
  return t ? { "Authorization": "Bearer " + t } : {}
}

export async function login(name: string): Promise<{ token: string; userName: string }> {
  const r = await fetch("/teamix/login", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name }),
  })
  const data = await r.json()
  if (data.error) throw new Error(data.error)
  localStorage.setItem(TOKEN_KEY, data.token)
  localStorage.setItem(USER_KEY, data.userName)
  return data
}

export function logout() {
  localStorage.removeItem(TOKEN_KEY)
  localStorage.removeItem(USER_KEY)
}

export function isLoggedIn(): boolean {
  return !!token()
}

export function currentUser(): string {
  return localStorage.getItem(USER_KEY) || ""
}

// Generic fetch helpers
async function get(path: string): Promise<any> {
  const r = await fetch(path + authQuery(), { headers: authHeaders() })
  if (!r.ok) throw new Error(await r.text())
  return r.json()
}

async function post(path: string, body?: any): Promise<any> {
  const r = await fetch(path + authQuery(), {
    method: "POST",
    headers: { "Content-Type": "application/json", ...authHeaders() },
    body: body ? JSON.stringify(body) : undefined,
  })
  if (!r.ok) throw new Error(await r.text())
  const ct = r.headers.get("content-type")
  if (ct && ct.includes("application/json")) return r.json()
  return null
}

export const api = {
  // Auth
  login,
  logout,
  isLoggedIn,
  currentUser,

  // Project
  project: (): Promise<any> => get("/teamix/project"),

  // User role
  userRole: (): Promise<any> => get("/teamix/user/role"),

  // Status
  status: (): Promise<import("../types").Status> => get("/status"),

  // History & Context
  history: (): Promise<any> => get("/history"),
  context: (): Promise<any> => get("/context"),

  // Sessions
  sessions: (): Promise<import("../types").Session[]> => get("/sessions"),

  // Models
  models: (): Promise<any> => get("/models"),

  // Checkpoints & Branches
  checkpoints: (): Promise<any> => get("/checkpoints"),
  branches: (): Promise<any> => get("/branches"),

  // Skills
  skills: (): Promise<import("../types").SkillsEntry[]> => get("/teamix/skills"),

  // MCP Servers
  mcpServers: (): Promise<import("../types").MCPServer[]> => get("/teamix/mcp/servers"),

  // Notifications
  notifications: (): Promise<import("../types").Notification[]> => get("/teamix/notifications"),

  // Workflow
  workflow: (): Promise<import("../types").WorkflowState> => get("/teamix/workflow"),
  workflowTemplates: (): Promise<any> => get("/teamix/workflows/templates"),
  workflowSelect: (template: string): Promise<any> => post("/teamix/workflows/select", { template }),
  workflowAdvance: (): Promise<any> => post("/teamix/workflow/advance", {}),
  workflowRollback: (): Promise<any> => post("/teamix/workflow/rollback", {}),
  workflowSetStage: (stage: string): Promise<any> => post("/teamix/workflow/setstage", { stage }),

  // Memory
  memoryList: (): Promise<any> => get("/teamix/memory"),
  memorySave: (m: import("../types").Memory): Promise<any> => post("/teamix/memory/save", m),

  // Config
  capabilities: (): Promise<any> => get("/teamix/capabilities"),
  capabilitiesSave: (kind: string, data: any): Promise<any> => post("/teamix/capabilities/save", { kind, data }),

  // Secrets
  secretsStatus: (): Promise<any> => get("/teamix/secrets/status"),
  secretsSet: (envName: string, value: string): Promise<any> => post("/teamix/secrets/set", { envName, value }),
  secretsDelete: (envName: string): Promise<any> => post("/teamix/secrets/delete", { envName }),

  // Key pool
  keyPoolStrategy: (strategy: string): Promise<any> => post("/teamix/keypool/strategy", { strategy }),

  // File tree
  tree: (): Promise<any> => get("/teamix/tree"),
  modules: (): Promise<any> => get("/teamix/modules"),

  // File content
  file: (path: string): Promise<any> => get("/teamix/file?path=" + encodeURIComponent(path)),

  // Agent actions
  submit: (input: string): Promise<void> => post("/submit", { input }),
  cancel: (): Promise<void> => post("/cancel"),
  approve: (id: string, allow: boolean): Promise<void> => post("/approve", { id, allow, session: false, persist: false }),
  plan: (on: boolean): Promise<void> => post("/plan", { on }),
  compact: (): Promise<void> => post("/compact"),
  newSession: (): Promise<void> => post("/new"),
  rewind: (turn: number): Promise<void> => post("/rewind", { turn }),
  fork: (turn: number): Promise<void> => post("/fork", { turn }),
  summarize: (turn?: number): Promise<void> => post("/summarize", turn ? { turn } : {}),
  goal: (goal: string): Promise<void> => post("/goal", { goal }),
  answer: (id: string, answers: any[]): Promise<void> => post("/answer", { id, answers }),
  resume: (path: string): Promise<void> => post("/resume", { path }),
  forget: (name: string): Promise<void> => post("/forget", { name }),
  deleteSession: (name: string): Promise<void> => post("/delete-session", { name }),
}
