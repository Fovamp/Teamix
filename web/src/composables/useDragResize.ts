// 垂直拖拽 resize 通用逻辑：从 ChatArea.vue 的 preview/composer 两套拖拽抽取。
// 使用方通过 getStartH/min/apply/onStart 注入差异，行为与原实现完全一致。
export interface VerticalDragResizeOptions {
  /** 拖拽开始时的初始高度 */
  getStartH(): number
  /** 计算下限：传入 startH，返回最小高度 */
  min(startH: number): number
  /** 上限 */
  max: number
  /** 应用新高度 */
  apply(h: number): void
  /** 拖拽开始时的额外初始化（如固定高度、加 class） */
  onStart?(e: MouseEvent): void
  /** 拖拽结束回调 */
  onStop?(): void
}

export function useVerticalDragResize(opts: VerticalDragResizeOptions) {
  const state = { active: false, startY: 0, startH: 0 }

  function onMove(e: MouseEvent) {
    if (!state.active) return
    const delta = state.startY - e.clientY
    const newH = Math.max(opts.min(state.startH), Math.min(opts.max, state.startH + delta))
    opts.apply(newH)
  }

  function onStop() {
    if (!state.active) return
    state.active = false
    document.removeEventListener('mousemove', onMove)
    document.removeEventListener('mouseup', onStop)
    opts.onStop?.()
  }

  function start(e: MouseEvent) {
    opts.onStart?.(e)
    state.active = true
    state.startY = e.clientY
    state.startH = opts.getStartH()
    document.addEventListener('mousemove', onMove)
    document.addEventListener('mouseup', onStop)
    e.preventDefault()
  }

  return { start }
}
