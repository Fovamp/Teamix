// 卡片类 DOM 渲染：从 ChatArea.vue 拆出（tool/approval/ask/compaction/usage/notice/phase/error/openpage/jump/stageApproval/wfConfirm）。
// 不持有任何组件状态：共享状态通过 CardsContext 注入，保证与原实现行为一致。
import { el, escHtml, fmtTok, fmtMoney, fmtElapsed, lineCount, toolArgsSummary } from "../utils/format"

export interface StageState { pending: boolean; reason: string; extra: string }

export interface CardsContext {
  /** #log 容器 */
  log(): HTMLElement | null
  /** 滚动到底部 */
  scrollDown(force?: boolean): void
  /** 隐藏欢迎页（含 hasVisibleHistory 同步） */
  hideWelcome(): void
  /** 获取/设置 pendingPrompts（approval/ask/openpage 清理用） */
  getPendingPrompts(): Function[]
  setPendingPrompts(fs: Function[]): void
  /** tool 卡片索引（id -> 卡片元素） */
  toolCards: Record<string, HTMLElement>
  /** 读取工作流阶段完成态 */
  getStageState(): StageState
  setStageState(s: Partial<StageState>): void
  /** 重新加载工作流（跳转/推进后调用） */
  onWorkflowChanged(): void
}

function toolIcon(kind: string): string {
  if (kind === 'success') return '<svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><polyline points="20 6 9 17 4 12"/></svg>'
  if (kind === 'danger') return '<svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>'
  return '<svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><circle cx="12" cy="12" r="10" stroke-dasharray="60" stroke-dashoffset="20"/></svg>'
}

function setToolStatus(card: HTMLElement, tone: string, label: string) {
  card.dataset.tone = tone
  const badge = card.querySelector('.card-badge')
  if (badge) badge.textContent = label
  const ico = card.querySelector('.ico')
  if (ico) { ico.className = 'ico' + (tone === 'accent' ? ' spin' : ''); ico.innerHTML = toolIcon(tone) }
}

async function copyText(text: string): Promise<boolean> {
  if (!text) return false
  if (navigator.clipboard?.writeText) return navigator.clipboard.writeText(text).then(() => true).catch(() => false)
  return false
}

export function createCards(ctx: CardsContext) {
  // insertCard 把卡片插入 #log 末尾（todo 面板已独立于会话流，不再作为锚点）。
  function insertCard(el: HTMLElement) {
    const log = ctx.log()
    if (!log) return
    const flowSelectors = '.card, .metric-strip, .approval, .ask, .compaction, .msg--error, .msg--assistant, .msg--user'
    const allFlow = log.querySelectorAll(flowSelectors)
    const anchor = allFlow.length > 0 ? allFlow[allFlow.length - 1] : null
    if (anchor && anchor.nextSibling) {
      log.insertBefore(el, anchor.nextSibling)
    } else {
      log.appendChild(el)
    }
  }
  // ── Tool cards ──
  function renderToolDispatch(tool: any) {
    ctx.hideWelcome()
    const card = el('div', 'card')
    card.id = 'tool-' + tool.id
    card.dataset.open = 'false'
    card.dataset.tone = 'accent'
    card.dataset.startedAt = String(Date.now())
    const head = el('div', 'card-head')
    const summary = toolArgsSummary(tool.args)
    head.innerHTML = '<span class="ico spin">' + toolIcon('accent') + '</span><div class="card-main"><div class="card-title"><span class="name">' + escHtml(tool.name) + '</span>' + (summary ? '<span class="subject">' + escHtml(summary) + '</span>' : '') + '</div><div class="card-meta">参数 ' + fmtTok(String(tool.args || '').length) + '</div></div><span class="card-badge">运行中</span><div class="card-actions"><button type="button" class="card-action card-copy" title="复制输出" aria-label="复制输出"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg></button><span class="chev"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="6 9 12 15 18 9"/></svg></span></div>'
    const body = el('div', 'card-body')
    body.style.display = 'none'
    head.onclick = (e) => {
      if ((e.target as HTMLElement).closest('button')) return
      const open = card.dataset.open === 'true'
      card.dataset.open = open ? 'false' : 'true'
      body.style.display = open ? 'none' : ''
    }
    const copy = head.querySelector('.card-copy') as HTMLElement
    if (copy) {
      copy.onclick = async (e) => {
        e.stopPropagation()
        const ok = await copyText(body.textContent || tool.args || '')
        copy.title = ok ? '已复制' : '复制输出'
        setTimeout(() => { copy.title = '复制输出' }, 1200)
      }
    }
    card.appendChild(head)
    card.appendChild(body)
    insertCard(card)
    ctx.toolCards[tool.id] = card
    ctx.scrollDown()
  }

  function renderToolResult(tool: any) {
    const card = ctx.toolCards[tool.id]
    if (!card) return
    const elapsed = Math.max(0, Date.now() - Number(card.dataset.startedAt || Date.now()))
    setToolStatus(card, tool.err ? 'danger' : 'success', tool.err ? '失败' : '完成')
    if (tool.err) {
      card.appendChild(el('div', 'err-body', tool.err))
      const meta = card.querySelector('.card-meta')
      if (meta) meta.textContent = fmtElapsed(elapsed)
    } else {
      const body = card.querySelector('.card-body')
      const output = String(tool.output || '')
      if (body) body.textContent = output ? output.slice(0, 2000) + (tool.truncated ? '\n...[truncated]' : '') : '无输出'
      const meta = card.querySelector('.card-meta')
      if (meta) meta.textContent = fmtElapsed(elapsed) + ' · ' + lineCount(output) + ' 行'
    }
    ctx.scrollDown()
  }

  function renderToolProgress(tool: any) {
    const card = ctx.toolCards[tool.id]
    if (!card) return
    const body = card.querySelector('.card-body') as HTMLElement
    if (!body) return
    body.style.display = ''
    card.dataset.open = 'true'
    body.textContent += tool.output || ''
    if (body.textContent.length > 4000) body.textContent = body.textContent.slice(-3000)
    const meta = card.querySelector('.card-meta')
    if (meta) meta.textContent = fmtElapsed(Date.now() - Number(card.dataset.startedAt || Date.now())) + ' · ' + lineCount(body.textContent) + ' 行'
    ctx.scrollDown()
  }

  // ── Approval card ──
  function showApproval(a: any) {
    const d = el('div', 'approval')
    const actions = [
      '<button class="approval__btn approval__btn--allow" data-allow="true" data-session="false"><span class="approval__key">Y</span> 允许</button>',
      '<button class="approval__btn approval__btn--allow" data-allow="true" data-session="true"><span class="approval__key">A</span> 本会话允许</button>',
      '<button class="approval__btn approval__btn--allow" data-allow="true" data-session="true" data-persist="true"><span class="approval__key">P</span> 总是允许</button>',
      '<button class="approval__btn approval__btn--deny" data-allow="false"><span class="approval__key">N</span> 拒绝</button>',
    ]
    d.innerHTML = '<div class="approval__header"><svg class="approval__icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg><span class="approval__title">需要批准</span></div><div class="approval__subject">' + escHtml(a.tool) + (a.subject ? ' — ' + escHtml(a.subject) : '') + '</div><div class="approval__actions">' + actions.join('') + '</div>'
    insertCard(d)
    ctx.scrollDown(true)
    const cleanup = () => { ctx.setPendingPrompts(ctx.getPendingPrompts().filter(f => f !== cleanup)); d.remove() }
    ctx.setPendingPrompts([...ctx.getPendingPrompts(), cleanup])
    const resolve = (payload: any) => {
      fetch('/approve?token=' + encodeURIComponent(localStorage.getItem('teamix_token') || ''), {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(Object.assign({ id: a.id }, payload))
      })
      cleanup()
    }
    d.querySelectorAll('.approval__btn').forEach(btn => {
      btn.addEventListener('click', () => resolve({
        allow: (btn as HTMLElement).dataset.allow === 'true',
        session: (btn as HTMLElement).dataset.session === 'true',
        persist: (btn as HTMLElement).dataset.persist === 'true',
      }))
    })
  }

  // ── Ask card ──
  function showAsk(ask: any) {
    const d = el('div', 'ask')
    const cleanup = () => { ctx.setPendingPrompts(ctx.getPendingPrompts().filter(f => f !== cleanup)); d.remove() }
    const questions = ask.questions
    let active = 0, sel = {}, custom = {}, customOpen = false, submitting = false

    const q = () => questions[Math.min(active, questions.length - 1)]
    const isLast = () => active >= questions.length - 1
    const progress = () => (active + 1) + '/' + questions.length

    function answerLabel(qi: number) {
        if (custom[qi] && custom[qi].trim()) return custom[qi].trim()
        return (sel[qi] || []).join(', ')
    }

    function setCustom(id: string, val: string) { custom[id] = val; if (val.trim()) sel[id] = [] }
    function canConfirm() {
        const cur = q()
        if (!cur || submitting) return false
        return (sel[cur.id] && sel[cur.id].length > 0) || (custom[cur.id] && custom[cur.id].trim()) || false
    }
    function advance() { active = Math.min(active + 1, questions.length - 1); customOpen = false; render() }

    function buildAnswers() {
        return questions.map((qq: any, qi: number) => ({
            questionId: qq.id,
            selected: custom[qq.id] && custom[qq.id].trim() ? [custom[qq.id].trim()] : (sel[qq.id] || [])
        }))
    }

    function confirmOrAdvance() {
        if (submitting || !canConfirm()) return
        if (isLast()) {
            submitting = true
            fetch('/answer?token=' + encodeURIComponent(localStorage.getItem('teamix_token') || ''), {
                method: 'POST', headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ id: ask.id, answers: buildAnswers() })
            })
            cleanup()
        } else { advance() }
    }

    function render() {
        d.querySelectorAll('.ask__body').forEach((e: any) => e.remove())
        const body = el('div', 'ask__body')
        const cur = q()
        if (!cur) return

        // Breadcrumbs for completed questions
        const crumbs = []
        for (let i = 0; i < active; i++) {
            const label = answerLabel(i)
            if (label) crumbs.push(el('span', 'ask__crumb', (i + 1) + '. ' + label))
        }
        if (crumbs.length) {
            const crumbDiv = el('div', 'ask__crumbs')
            crumbs.forEach((c: any) => crumbDiv.appendChild(c))
            body.appendChild(crumbDiv)
        }

        // Header with progress
        const header = el('div', 'ask__header')
        header.innerHTML = (cur.header ? '<span class="ask__header-text">' + escHtml(cur.header) + '</span>' : '')
            + (questions.length > 1 ? '<span class="ask__progress">' + progress() + '</span>' : '')
        body.appendChild(header)
        body.appendChild(el('div', 'ask__prompt', cur.prompt))

        // Options with number keys
        const opts = el('div', 'ask__options')
        cur.options.forEach((o: any, i: number) => {
            const opt = el('button', 'ask__opt')
            const isSel = (sel[cur.id] || []).includes(o.label)
            if (isSel) opt.classList.add('ask__opt--selected')
            opt.innerHTML = '<span class="ask__opt-num">' + (i + 1) + '</span><div><div class="ask__opt-label">' + escHtml(o.label) + '</div>'
                + (o.description ? '<div class="ask__opt-desc">' + escHtml(o.description) + '</div>' : '') + '</div>'
            opt.onclick = () => {
                if (submitting) return
                setCustom(cur.id, '')
                if (cur.multi) {
                    const s = sel[cur.id] || []
                    sel[cur.id] = s.includes(o.label) ? s.filter((x: string) => x !== o.label) : [...s, o.label]
                } else { sel[cur.id] = [o.label] }
                customOpen = false
                render()
            }
            opts.appendChild(opt)
        })

        // Custom answer row
        const customRow = el('button', 'ask__opt')
        if (customOpen) customRow.classList.add('ask__opt--selected')
        customRow.innerHTML = '<div><div class="ask__opt-label">自定义回答</div><div class="ask__opt-desc">输入自由文字回答</div></div>'
        customRow.onclick = () => { if (!submitting) { customOpen = !customOpen; sel[cur.id] = []; render() } }
        opts.appendChild(customRow)

        // Skip row
        const skipRow = el('button', 'ask__opt', '直接聊天（跳过）')
        skipRow.style.color = 'var(--danger)'
        skipRow.onclick = () => {
            if (submitting) return
            if (isLast()) {
                submitting = true
                fetch('/answer?token=' + encodeURIComponent(localStorage.getItem('teamix_token') || ''), {
                    method: 'POST', headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ id: ask.id, answers: buildAnswers() })
                })
                cleanup()
                return
            }
            advance()
        }
        opts.appendChild(skipRow)
        body.appendChild(opts)

        // Free text input (only when custom is open)
        if (customOpen) {
            const input = el('textarea', 'ask__free') as HTMLTextAreaElement
            input.placeholder = '输入自定义回答...'
            input.value = custom[cur.id] || ''
            input.oninput = () => {
                setCustom(cur.id, input.value)
                // 不重建 DOM：重建会替换输入框节点、中断中文输入法组合（候选框闪没），
                // 只同步提交按钮的禁用态
                const submit = d.querySelector('.ask__submit') as HTMLButtonElement | null
                if (submit) submit.disabled = !canConfirm()
            }
            input.onkeydown = (e) => { if (e.key === 'Enter' && !e.shiftKey && custom[cur.id] && custom[cur.id].trim()) { e.preventDefault(); confirmOrAdvance() } }
            body.appendChild(input)
        }

        // Footer buttons
        const footer = el('div', 'ask__footer')
        if (active > 0) {
            const back = el('button', 'ask__back', '← 上一问')
            back.onclick = () => { if (!submitting) { active = Math.max(0, active - 1); customOpen = false; render() } }
            footer.appendChild(back)
        }
        const btn = el('button', 'ask__submit', isLast() ? '提交' : '下一问 →')
        btn.disabled = !canConfirm()
        btn.onclick = confirmOrAdvance
        footer.appendChild(btn)
        body.appendChild(footer)

        d.appendChild(body)
        // render() 重建 body 会替换输入框节点，输入时立即恢复焦点避免失焦
        if (customOpen) {
            const inp = body.querySelector('.ask__free') as HTMLTextAreaElement | null
            if (inp) inp.focus()
        }
    }

    insertCard(d)
    ctx.scrollDown(true)
    render()
    ctx.setPendingPrompts([...ctx.getPendingPrompts(), cleanup])
}

// ── Compaction ──
  function showCompaction(c: any) {
    const d = el('div', 'compaction')
    if (c.summary) {
      const head = el('div', 'compaction__head')
      head.innerHTML = '<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="4 14 10 14 10 20"/><polyline points="20 10 14 10 14 4"/><line x1="14" y1="10" x2="21" y2="3"/><line x1="3" y1="21" x2="10" y2="14"/></svg>'
      head.appendChild(el('span', 'compaction__title', '已压缩'))
      head.appendChild(el('span', '', c.messages + ' 条消息'))
      const body = el('div', 'compaction__body', c.summary)
      head.onclick = () => body.classList.toggle('compaction__body--open')
      d.appendChild(head); d.appendChild(body)
    } else {
      d.textContent = '压缩中...'
    }
    insertCard(d)
    ctx.scrollDown()
  }

  // ── Usage strip ──
  function showUsageStrip(usage: any) {
    try { sessionStorage.setItem('teamix_last_usage', JSON.stringify(usage)) } catch { /* ignore */ }
    const log = ctx.log()
    if (!log) return
    const strip = el('div', 'metric-strip')
    const items = [
      { l: '总计', v: fmtTok(usage.totalTokens), c: '' },
      { l: '输入', v: fmtTok(usage.promptTokens), c: 'acc' },
      { l: '输出', v: fmtTok(usage.completionTokens), c: 'ok' },
    ]
    items.forEach(it => {
      const sp = el('span', 'item')
      sp.innerHTML = it.l + ' <span class="v ' + it.c + '">' + it.v + '</span>'
      strip.appendChild(sp)
    })
    if (usage.cacheHitTokens) {
      const sp = el('span', 'item')
      sp.innerHTML = '缓存 <span class="v acc">' + Math.round(usage.cacheHitTokens / Math.max(1, usage.cacheHitTokens + usage.cacheMissTokens) * 100) + '%</span>'
      strip.appendChild(sp)
    }
    const cost = usage.cost ?? usage.costUsd
    if (typeof cost === 'number' && cost > 0) {
      const sp = el('span', 'item')
      sp.innerHTML = '费用 <span class="v">' + fmtMoney(cost, usage.currency) + '</span>'
      strip.appendChild(sp)
    }
    insertCard(strip)
    ctx.scrollDown()
  }

  // ── Notice / Phase / Error ──
  function showNotice(text: string, tone?: string) {
    ctx.hideWelcome()
    // 系统级过程通知（会话冲突副本 / 后台 bash 启停）不占会话卡片，避免堆积
    if (/^(session changed on disk|background bash (started|killed):)/.test(text)) return
    // 工具输出截断提示：不占独立卡片（避免大输出堆积在会话流底部），
    // 附加到最近的 tool 卡片元信息上；无对应卡片时弱化为小字提示。
    if (/^tool output truncated:/.test(text)) {
      const cards = Object.values(ctx.toolCards)
      if (cards.length > 0) {
        const last = cards[cards.length - 1]
        const meta = last.querySelector('.card-meta')
        if (meta) meta.textContent += ' · 输出已截断'
        return
      }
      const muted = el('div', 'notice notice--muted', text)
      insertCard(muted)
      ctx.scrollDown(true)
      return
    }
    const n = el('div', 'notice' + (tone === 'warn' ? ' notice--warn' : ''), text)
    insertCard(n)
    ctx.scrollDown(true)
  }

  function showPhase(text: string) {
    insertCard(el('div', 'phase', text))
    ctx.scrollDown()
  }

  function showError(err: string) {
    const log = ctx.log()
    if (log) log.appendChild(el('div', 'msg--error', '✗ ' + err))
    ctx.scrollDown()
  }

  // ── Open page card ──
  function showOpenPageCard(pages: { url: string; label: string }[]) {
    const d = el('div', 'approval')
    d.style.borderLeft = '3px solid var(--accent)'
    d.style.marginBottom = '8px'
    const btns = pages.map((p, i) => '<button class="approval__btn approval__btn--allow" id="opg-' + i + '"><span class="approval__key">' + (i + 1) + '</span> 打开 ' + escHtml(p.label) + '</button>').join('')
    const list = pages.map(p => '<div style="font-family:var(--mono);font-size:11px;color:var(--fg-2);padding:2px 0">• ' + p.url + ' <span style="color:var(--muted-2)">(' + p.label + ')</span></div>').join('')
    d.innerHTML = '<div class="approval__header"><svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/><polyline points="15 3 21 3 21 9"/><line x1="10" y1="14" x2="21" y2="3"/></svg><span class="approval__title">查看效果</span></div><div class="approval__subject">修改已完成，请检查效果：</div><div style="padding:6px 12px 8px;border-bottom:1px solid var(--border);background:var(--bg-2)">' + list + '</div><div class="approval__actions">' + btns + '<button class="approval__btn approval__btn--deny" id="opg-dismiss" style="margin-left:auto">暂不打开</button></div>'
    insertCard(d)
    ctx.scrollDown(true)
    ctx.setPendingPrompts([...ctx.getPendingPrompts(), () => { if (d.parentNode) d.remove() }])
    pages.forEach((p, i) => {
      document.getElementById('opg-' + i)?.addEventListener('click', () => { window.open(p.url, '_blank') })
    })
    document.getElementById('opg-dismiss')?.addEventListener('click', () => { d.remove() })
  }

  // ── Wf jump card ──
  function showJumpCard() {
    const d = el('div', 'approval')
    d.style.borderLeft = '3px solid var(--accent)'
    d.style.marginBottom = '8px'
    d.innerHTML = '<div class="approval__header"><svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg><span class="approval__title">跳转到阶段</span></div><div class="approval__subject">请选择要跳转到的阶段：</div><div id="jump-stage-list" style="padding:6px 12px;border-bottom:1px solid var(--border);background:var(--bg-2)"></div><div class="approval__actions"><button class="approval__btn approval__btn--deny" id="jump-cancel">取消</button></div>'
    insertCard(d)
    ctx.scrollDown(true)
    document.getElementById('jump-cancel')!.onclick = () => { d.remove() }
    const list = document.getElementById('jump-stage-list')!
    list.innerHTML = '<div style="color:var(--muted-2);padding:4px 0">加载中...</div>'
    const t = localStorage.getItem('teamix_token')
    if (!t) return
    fetch('/teamix/workflow?token=' + encodeURIComponent(t)).then(r => r.json()).then(data => {
      if (!data || !data.stages || data.stages.length === 0) { list.innerHTML = '<div style="color:var(--muted-2);padding:4px 0">无可用阶段</div>'; return }
      let html = ''
      data.stages.forEach((s: any) => {
        const active = s.status === 'in_progress' ? ' style="background:var(--accent-soft);color:var(--accent);font-weight:500"' : ''
        html += '<div class="wf-jump-item" data-stage="' + s.stage + '"' + active + ' style="padding:5px 8px;cursor:pointer;border-radius:4px;margin:2px 0">' + (s.label || s.stage) + ' <span style="color:var(--muted-2);font-size:10px">(' + (s.status === 'in_progress' ? '当前' : s.status === 'completed' ? '已完成' : '待开始') + ')</span></div>'
      })
      list.innerHTML = html
      list.querySelectorAll('.wf-jump-item').forEach((elItem) => {
        (elItem as HTMLElement).onclick = () => {
          const sn = elItem.getAttribute('data-stage')
          const tk = localStorage.getItem('teamix_token')
          if (!sn || !tk) return
          fetch('/teamix/workflow/setstage?token=' + encodeURIComponent(tk), { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ stage: sn }) })
            .then(r => {
              if (r.ok) { d.remove(); ctx.onWorkflowChanged() }
            })
        }
      })
    }).catch(() => { list.innerHTML = '<div style="color:#f44336;padding:4px 0">加载失败</div>' })
  }

  // ── Stage approval ──
  function showStageApproval(reason: string) {
    const st = ctx.getStageState()
    const extra = st.extra ? '\n\n' + st.extra : ''
    const msg = (reason ? 'AI认为当前阶段已完成：' + reason : 'AI认为当前阶段已完成') + extra
    const d = el('div', 'approval')
    d.style.borderLeft = '3px solid var(--accent)'
    d.style.marginBottom = '8px'
    d.innerHTML = '<div class="approval__header"><svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg><span class="approval__title">阶段完成</span></div><div class="approval__subject">' + msg + '</div><div class="approval__actions"><button class="approval__btn approval__btn--allow" id="sa-confirm"><span class="approval__key">Y</span> 确认进入下一阶段</button><button class="approval__btn approval__btn--deny" id="sa-cancel"><span class="approval__key">N</span> 继续当前阶段</button><button class="approval__btn" id="sa-jump" style="border:1px solid var(--border);color:var(--fg-2);font-size:11px"><span class="approval__key">J</span> 跳转到...</button></div><div id="sa-jump-list" style="display:none;border-top:1px solid var(--border);padding:8px 12px;font-size:12px"></div>'
    insertCard(d)
    ctx.scrollDown(true)
    document.getElementById('sa-confirm')!.onclick = () => {
      ctx.setStageState({ pending: false, reason: '', extra: '' })
      d.remove()
      const t = localStorage.getItem('teamix_token')
      if (!t) return
      fetch('/teamix/workflow/advance?token=' + encodeURIComponent(t), { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' })
        .then(() => { ctx.onWorkflowChanged(); fetch('/submit?token=' + encodeURIComponent(t), { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ input: '请继续' }) }) })
        .catch(() => { ctx.onWorkflowChanged(); fetch('/submit?token=' + encodeURIComponent(t), { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ input: '请继续' }) }) })
    }
    document.getElementById('sa-cancel')!.onclick = () => {
      ctx.setStageState({ pending: false, reason: '', extra: '' })
      d.remove()
    }
    document.getElementById('sa-jump')!.onclick = () => {
      const list = document.getElementById('sa-jump-list')!
      if (list.style.display !== 'none') { list.style.display = 'none'; return }
      list.innerHTML = '<div style="color:var(--muted-2);padding:4px 0">加载中...</div>'
      list.style.display = 'block'
      const t = localStorage.getItem('teamix_token')
      if (!t) return
      fetch('/teamix/workflow?token=' + encodeURIComponent(t)).then(r => r.json()).then(data => {
        if (!data || !data.stages || data.stages.length === 0) { list.innerHTML = '<div style="color:var(--muted-2);padding:4px 0">无可用阶段</div>'; return }
        let html = ''
        data.stages.forEach((s: any) => {
          const active = s.status === 'in_progress' ? ' style="background:var(--accent-soft);color:var(--accent);font-weight:500"' : ''
          html += '<div class="wf-jump-item" data-stage="' + s.stage + '"' + active + ' style="padding:5px 8px;cursor:pointer;border-radius:4px;margin:2px 0">' + (s.label || s.stage) + ' <span style="color:var(--muted-2);font-size:10px">(' + s.status + ')</span></div>'
        })
        list.innerHTML = html
        list.querySelectorAll('.wf-jump-item').forEach((elItem) => {
          (elItem as HTMLElement).onclick = () => {
            const sn = elItem.getAttribute('data-stage')
            const tk = localStorage.getItem('teamix_token')
            if (!sn || !tk) return
            fetch('/teamix/workflow/setstage?token=' + encodeURIComponent(tk), { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ stage: sn }) })
              .then(r => { if (r.ok) { ctx.setStageState({ pending: false }); d.remove(); ctx.onWorkflowChanged(); fetch('/submit?token=' + encodeURIComponent(tk), { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ input: '请继续' }) }) } })
          }
        })
      }).catch(() => { list.innerHTML = '<div style="color:#f44336;padding:4px 0">加载失败</div>' })
    }
  }

  // ── Wf confirm ──
  function showWfConfirm(msg: string, callback: (ok: boolean) => void) {
    const d = el('div', 'approval')
    d.style.borderLeft = '3px solid var(--accent)'
    d.style.marginBottom = '8px'
    d.innerHTML = '<div class="approval__header"><svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg><span class="approval__title">工作流确认</span></div><div class="approval__subject">' + msg + '</div><div class="approval__actions"><button class="approval__btn approval__btn--allow" id="wfc-yes"><span class="approval__key">Y</span> 确认</button><button class="approval__btn approval__btn--deny" id="wfc-no"><span class="approval__key">N</span> 取消</button></div>'
    insertCard(d)
    ctx.scrollDown(true)
    const cleanup = () => { d.remove() }
    document.getElementById('wfc-yes')!.onclick = () => { cleanup(); callback(true) }
    document.getElementById('wfc-no')!.onclick = () => { cleanup(); callback(false) }
  }

  return { renderToolDispatch, renderToolResult, renderToolProgress, showApproval, showAsk, showCompaction, showUsageStrip, showNotice, showPhase, showError, showOpenPageCard, showJumpCard, showStageApproval, showWfConfirm }
}
