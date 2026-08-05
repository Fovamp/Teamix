// 全局交互增强（App 挂载时调用）：
// 1. 禁用浏览器原生右键菜单（contextmenu）。
// 2. 左键长按文本 → 在鼠标右侧弹出"复制"气泡 → 点击复制（选中的文本，
//    否则复制长按目标的整段文本），替代被禁用的右键复制。

let pressTimer: number | null = null
let pressX = 0
let pressY = 0
let pressTarget: HTMLElement | null = null
let bubble: HTMLDivElement | null = null

function hideBubble() {
  if (bubble) bubble.style.display = "none"
}

function ensureBubble(): HTMLDivElement {
  if (bubble) return bubble
  bubble = document.createElement("div")
  bubble.id = "longpress-copy"
  bubble.textContent = "复制"
  Object.assign(bubble.style, {
    position: "fixed",
    zIndex: "9999",
    display: "none",
    cursor: "pointer",
    background: "rgba(28,32,40,.94)",
    color: "#fff",
    padding: "6px 16px",
    borderRadius: "6px",
    fontSize: "12px",
    boxShadow: "0 3px 12px rgba(0,0,0,.4)",
    userSelect: "none",
    fontFamily: "inherit",
  } as CSSStyleDeclaration)
  bubble.addEventListener("mousedown", (e) => {
    e.stopPropagation()
    e.preventDefault()
  })
  bubble.addEventListener("click", () => {
    const text = resolveCopyText()
    if (text) navigator.clipboard?.writeText(text).catch(() => {})
    hideBubble()
  })
  document.body.appendChild(bubble)
  return bubble
}

function resolveCopyText(): string {
  const sel = window.getSelection()
  const selText = sel && sel.toString() ? sel.toString().trim() : ""
  if (selText) return selText
  if (pressTarget) {
    const text = (pressTarget.innerText || pressTarget.textContent || "").trim()
    if (text) return text
  }
  return ""
}

export function initGlobalInteraction() {
  // 禁用全局浏览器右键菜单（输入类元素除外，保留原生右键粘贴/拼写检查）
  document.addEventListener("contextmenu", (e) => {
    const t = e.target as HTMLElement
    if (t && t.closest("input, textarea, [contenteditable]")) return
    e.preventDefault()
  })
  ensureBubble()
  document.addEventListener("mousedown", (e) => {
    if (e.button !== 0) return
    const t = e.target as HTMLElement
    if (!t || t.closest("input, textarea, select, [contenteditable], button, a, .modal-overlay, .session-del, .ctx-menu, #longpress-copy")) {
      return
    }
    hideBubble()
    pressX = e.clientX
    pressY = e.clientY
    pressTarget = t
    pressTimer = window.setTimeout(() => {
      const b = ensureBubble()
      b.style.left = Math.min(pressX + 14, window.innerWidth - 90) + "px"
      b.style.top = pressY + 10 + "px"
      b.style.display = "block"
      // 不点击的话几秒后自动隐藏，避免气泡残留
      window.setTimeout(hideBubble, 3000)
    }, 600)
  })
  document.addEventListener("mousemove", (e) => {
    if (pressTimer !== null && (Math.abs(e.clientX - pressX) > 10 || Math.abs(e.clientY - pressY) > 10)) {
      clearTimeout(pressTimer)
      pressTimer = null
    }
  })
  document.addEventListener("mouseup", () => {
    if (pressTimer !== null) {
      clearTimeout(pressTimer)
      pressTimer = null
    }
  })
}
