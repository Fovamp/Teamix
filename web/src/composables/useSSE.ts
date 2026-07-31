// SSE 客户端：从 ChatArea.vue 的 connectSSE 拆出。
// 只负责连接生命周期（创建/关闭/错误状态映射）；事件解析与业务分发通过回调注入。
export type SSEConnState = 'reconnecting' | 'disconnected'

export interface SSEHandlers {
  /** 连接打开（含每条消息到达时也可自行置 connected） */
  onOpen(): void
  /** 收到原始消息事件（含 JSON.parse 与业务分发） */
  onMessage(evt: MessageEvent): void
  /** 连接错误（readyState 为 CONNECTING 时是 reconnecting，否则 disconnected） */
  onError(state: SSEConnState): void
}

export function useSSE(handlers: SSEHandlers) {
  let es: EventSource | null = null

  function connect(): void {
    const t = localStorage.getItem('teamix_token')
    if (!t) { console.log('no token for SSE'); return }
    const url = '/events?token=' + encodeURIComponent(t)
    console.log('SSE connecting to', url)
    es = new EventSource(url)
    es.onopen = () => { handlers.onOpen() }
    es.onmessage = (evt) => { handlers.onMessage(evt) }
    es.onerror = () => {
      if (es?.readyState === EventSource.CONNECTING) handlers.onError('reconnecting')
      else handlers.onError('disconnected')
    }
  }

  function close(): void {
    if (es) es.close()
  }

  return { connect, close }
}
