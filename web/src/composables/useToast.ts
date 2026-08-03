import { reactive } from "vue"

export interface ToastItem {
  id: number
  msg: string
  type: "success" | "error" | "info"
}

// 全局 toast 状态：任何组件调用 useToast().toast() 即可弹出提示，
// ToastContainer.vue 在 App.vue 挂载渲染，无需每个页面自建。
const state = reactive<{ items: ToastItem[] }>({ items: [] })
let seq = 0

export function useToast() {
  function toast(msg: string, type: ToastItem["type"] = "error", duration = 4000) {
    const id = ++seq
    state.items.push({ id, msg, type })
    setTimeout(() => {
      const i = state.items.findIndex((t) => t.id === id)
      if (i >= 0) state.items.splice(i, 1)
    }, duration)
  }
  return { toast, toastState: state }
}
