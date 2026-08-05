// 常量：从 ChatArea.vue 拆出。

/** rewind 操作范围（仅回退相关：代码/对话的组合；分叉、总结是独立功能，不混入） */
export const SCOPES = [
  { key: 'b', label: '代码 + 对话', scope: 'both' },
  { key: 'c', label: '仅对话', scope: 'conversation' },
  { key: 'd', label: '仅代码', scope: 'code' },
]

/** slash 命令面板 */
export const SLASH_CMDS = [
  { cmd: 'new', sig: '/new', desc: '新建会话', group: 'session' },
  { cmd: 'resume', sig: '/resume [n]', desc: '恢复会话', group: 'session' },
  { cmd: 'compact', sig: '/compact', desc: '压缩对话', group: 'session' },
  { cmd: 'rewind', sig: '/rewind', desc: '回退到检查点', group: 'session' },
  { cmd: 'tree', sig: '/tree', desc: '显示分支树', group: 'branch' },
  { cmd: 'branch', sig: '/branch <name>', desc: '创建分支', group: 'branch' },
  { cmd: 'switch', sig: '/switch <id>', desc: '切换分支', group: 'branch' },
  { cmd: 'model', sig: '/model [provider/model]', desc: '列出/切换模型', group: 'model' },
  { cmd: 'effort', sig: '/effort <level>', desc: '推理努力级别', group: 'model' },
  { cmd: 'goal', sig: '/goal <task>', desc: '设置目标让代理自主执行', group: 'agent' },
  { cmd: 'thinking', sig: '/thinking <level>', desc: '思考努力', group: 'agent' },
  { cmd: 'verbose', sig: '/verbose', desc: '切换推理显示', group: 'agent' },
  { cmd: 'mcp', sig: '/mcp', desc: 'MCP 服务器', group: 'system' },
  { cmd: 'skill', sig: '/skill', desc: '技能', group: 'system' },
  { cmd: 'hooks', sig: '/hooks', desc: '钩子', group: 'system' },
  { cmd: 'memory', sig: '/memory', desc: '显示记忆', group: 'memory' },
  { cmd: 'forget', sig: '/forget <item>', desc: '忘记记忆', group: 'memory', danger: true },
  { cmd: 'help', sig: '/help', desc: '帮助', group: 'help' },
]

/** slash 分组显示名 */
export const SLASH_GROUP_NAMES: Record<string, string> = {
  session: '会话', branch: '分支', model: '模型', agent: '代理', system: '系统', memory: '记忆', help: '帮助',
}
