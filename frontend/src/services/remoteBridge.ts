// 远程后端桥接：桌面探测到 NAS 后端可达时(GetBackendMode 返回 remote)，
// 把前端对内置后端的调用改道到 NAS：
//   - window.go.main.App.X(...args)  →  POST <nas>/rpc/X  body=[...args]
//   - 事件通过 WebSocket：下行 {type:event} 派发给本地 EventsOn 监听者；
//     上行 EventsEmit(如 market:subscribe) 通过 WS 送到 NAS 的 rt.On 订阅者。
// 连不上则保持 Wails 内置绑定(本地全量)不动。

type AnyFn = (...args: any[]) => any

interface Listener {
  cb: AnyFn
  max: number // -1 表示无限
  count: number
}

// 探测后端模式：调用真实的内置绑定 GetBackendMode。
export async function resolveBackendMode(timeoutMs = 4000): Promise<{ mode: string; url?: string; token?: string }> {
  const start = Date.now()
  // 等 Wails 注入 window.go.main.App.GetBackendMode
  while (Date.now() - start < timeoutMs) {
    const app = (window as any).go?.main?.App
    if (app && typeof app.GetBackendMode === 'function') {
      try {
        const m = await app.GetBackendMode()
        return m || { mode: 'local' }
      } catch {
        return { mode: 'local' }
      }
    }
    await new Promise((r) => setTimeout(r, 50))
  }
  return { mode: 'local' }
}

// 显示"离线回落"提示横幅：配了 NAS 但没连上，当前用的是本地旧数据，改动不会同步到 NAS。
// 纯 DOM 注入，不依赖 React 状态，顶部居中(避开无边框标题栏的窗口按钮)，可手动关闭。
export function showOfflineBanner(url: string) {
  if (document.getElementById('jcp-offline-banner')) return
  const bar = document.createElement('div')
  bar.id = 'jcp-offline-banner'
  bar.style.cssText = [
    'position:fixed',
    'top:44px',
    'left:50%',
    'transform:translateX(-50%)',
    'z-index:99999',
    'max-width:90vw',
    'display:flex',
    'align-items:center',
    'gap:10px',
    'padding:8px 14px',
    'border-radius:10px',
    'background:rgba(180,83,9,.96)',
    'color:#fff',
    'font-size:13px',
    'line-height:1.4',
    'font-family:system-ui,-apple-system,Segoe UI,Roboto,sans-serif',
    'box-shadow:0 6px 20px rgba(0,0,0,.35)',
    'border:1px solid rgba(253,186,116,.6)',
  ].join(';')
  const text = document.createElement('span')
  text.innerHTML =
    '⚠️ 当前离线：NAS 后端(' +
    url.replace(/</g, '&lt;') +
    ')未连接，正在使用<b>本地旧数据</b>，此时的改动<b>不会同步到 NAS</b>。'
  const close = document.createElement('button')
  close.textContent = '知道了'
  close.style.cssText = [
    'flex:none',
    'cursor:pointer',
    'border:none',
    'border-radius:6px',
    'padding:3px 10px',
    'font-size:12px',
    'background:rgba(255,255,255,.22)',
    'color:#fff',
  ].join(';')
  close.onclick = () => bar.remove()
  bar.appendChild(text)
  bar.appendChild(close)
  const mount = () => document.body && document.body.appendChild(bar)
  if (document.body) mount()
  else document.addEventListener('DOMContentLoaded', mount)
}

// 安装远程桥接。url 形如 http://192.168.1.4:8810
// 访客登录层:分发版 app 没有主人令牌,401 时弹账号密码,Login 换会话令牌存本地。
const SESSION_KEY = 'jcp_session_token'

export function showLoginOverlay(base: string) {
  if (document.getElementById('jcp-login-overlay')) return
  const ov = document.createElement('div')
  ov.id = 'jcp-login-overlay'
  ov.style.cssText =
    'position:fixed;inset:0;z-index:100000;display:flex;align-items:center;justify-content:center;background:rgba(3,7,15,.88);backdrop-filter:blur(6px);font-family:system-ui,-apple-system,Segoe UI,Roboto,sans-serif;'
  const inputCss =
    'margin-top:10px;width:100%;box-sizing:border-box;padding:9px 12px;border-radius:8px;border:1px solid rgba(148,163,184,.3);background:#060d1a;color:#e2e8f0;font-size:14px;outline:none;'
  ov.innerHTML = `
    <div style="width:320px;padding:26px 26px 22px;border:1px solid rgba(148,163,184,.25);border-radius:14px;background:#0b1524;color:#e2e8f0;box-shadow:0 20px 60px rgba(0,0,0,.5);">
      <div id="jcp-login-title" style="font-size:17px;font-weight:700;">JOEY · 登录</div>
      <div style="margin-top:4px;font-size:12px;color:#94a3b8;">连接远程服务需要账号</div>
      <input id="jcp-login-user" placeholder="账号" autocomplete="username" style="margin-top:16px;${inputCss}">
      <input id="jcp-login-pass" placeholder="密码" type="password" autocomplete="current-password" style="${inputCss}">
      <input id="jcp-login-pass2" placeholder="确认密码" type="password" autocomplete="new-password" style="display:none;${inputCss}">
      <input id="jcp-login-invite" placeholder="邀请码(如无可留空)" style="display:none;${inputCss}">
      <div id="jcp-login-err" style="margin-top:8px;min-height:16px;font-size:12px;color:#f87171;"></div>
      <button id="jcp-login-btn" style="margin-top:8px;width:100%;padding:10px;border:none;border-radius:8px;background:#0ea5e9;color:#fff;font-size:14px;font-weight:700;cursor:pointer;">登 录</button>
      <div style="margin-top:12px;text-align:center;font-size:12px;color:#94a3b8;">
        <a id="jcp-login-toggle" style="color:#38bdf8;cursor:pointer;">没有账号?注册一个</a>
      </div>
    </div>`
  const mount = () => document.body && document.body.appendChild(ov)
  if (document.body) mount()
  else document.addEventListener('DOMContentLoaded', mount)

  let registerMode = false
  const setMode = (reg: boolean) => {
    registerMode = reg
    const show = (id: string, on: boolean) => {
      const el = document.getElementById(id) as HTMLElement | null
      if (el) el.style.display = on ? '' : 'none'
    }
    show('jcp-login-pass2', reg)
    show('jcp-login-invite', reg)
    document.getElementById('jcp-login-title')!.textContent = reg ? 'JOEY · 注册' : 'JOEY · 登录'
    ;(document.getElementById('jcp-login-btn') as HTMLButtonElement).textContent = reg ? '注 册' : '登 录'
    document.getElementById('jcp-login-toggle')!.textContent = reg ? '已有账号?去登录' : '没有账号?注册一个'
    document.getElementById('jcp-login-err')!.textContent = ''
  }

  const doSubmit = async () => {
    const val = (id: string) => (document.getElementById(id) as HTMLInputElement)?.value ?? ''
    const u = val('jcp-login-user').trim()
    const p = val('jcp-login-pass')
    const err = document.getElementById('jcp-login-err')!
    if (!u || !p) {
      err.textContent = '请输入账号和密码'
      return
    }
    if (registerMode && p !== val('jcp-login-pass2')) {
      err.textContent = '两次输入的密码不一致'
      return
    }
    err.textContent = ''
    const btn = document.getElementById('jcp-login-btn') as HTMLButtonElement
    btn.disabled = true
    btn.textContent = registerMode ? '注册中…' : '登录中…'
    try {
      const method = registerMode ? 'Register' : 'Login'
      const args = registerMode ? [u, p, val('jcp-login-invite').trim()] : [u, p]
      const resp = await fetch(`${base}/rpc/${method}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(args),
      })
      const data = await resp.json()
      if (data && data.success && data.token) {
        localStorage.setItem(SESSION_KEY, data.token)
        window.location.reload()
        return
      }
      err.textContent = (data && data.error) || (registerMode ? '注册失败' : '登录失败')
    } catch {
      err.textContent = '网络异常,请重试'
    }
    btn.disabled = false
    btn.textContent = registerMode ? '注 册' : '登 录'
  }
  setTimeout(() => {
    document.getElementById('jcp-login-btn')?.addEventListener('click', doSubmit)
    document.getElementById('jcp-login-toggle')?.addEventListener('click', () => setMode(!registerMode))
    ov.addEventListener('keydown', (e) => {
      if ((e as KeyboardEvent).key === 'Enter') doSubmit()
    })
  }, 50)
}

export function installRemoteBridge(url: string, token?: string) {
  const base = url.replace(/\/+$/, '')
  // 凭证:主人令牌(config 下发) > 访客会话令牌(登录后存 localStorage)
  const cred = token || localStorage.getItem(SESSION_KEY) || ''
  // 暴露给需要拼下载链接的功能(如投研报告 Word 文件 /reports/);token 供下载链接带 ?token=
  ;(window as any).__jcpRemoteBase = base
  ;(window as any).__jcpRemoteToken = cred

  // ---- 1) RPC 代理：接管 window.go.main.App ----
  const rpc = async (method: string, args: any[]) => {
    const headers: Record<string, string> = { 'Content-Type': 'application/json' }
    if (cred) headers['X-JCP-Token'] = cred
    const resp = await fetch(`${base}/rpc/${method}`, {
      method: 'POST',
      headers,
      body: JSON.stringify(args ?? []),
    })
    // 凭证无效/过期(或分发版首次使用):弹登录层换会话令牌
    if (resp.status === 401 && !token) {
      localStorage.removeItem(SESSION_KEY)
      showLoginOverlay(base)
    }
    const text = await resp.text()
    let data: any = null
    if (text) {
      try {
        data = JSON.parse(text)
      } catch {
        data = text
      }
    }
    if (!resp.ok) {
      const msg = (data && data.error) || `RPC ${method} 失败(${resp.status})`
      throw new Error(msg)
    }
    return data
  }

  const w = window as any
  // 装桥前先抓住本地真 Wails 绑定：窗口控制/打开浏览器/自更新这些是本地桌面操作，
  // 必须走本地，不能转发到无头的 NAS(NAS 上是空操作，点了没反应)。
  const localApp = w.go?.main?.App
  const LOCAL_METHODS = new Set([
    'OpenURL',
    'WindowMinimize',
    'WindowMaximize',
    'WindowClose',
    'CheckForUpdate',
    'DoUpdate',
    'RestartApp',
    'GetCurrentVersion',
  ])

  const appProxy = new Proxy(
    {},
    {
      get: (_t, prop) => {
        if (typeof prop !== 'string') return undefined
        if (LOCAL_METHODS.has(prop) && localApp && typeof localApp[prop] === 'function') {
          return (...args: any[]) => localApp[prop](...args)
        }
        return (...args: any[]) => rpc(prop, args)
      },
    }
  )
  w.go = w.go || {}
  w.go.main = w.go.main || {}
  w.go.main.App = appProxy

  // ---- 2) 事件总线：自带注册表接管 window.runtime.Events* ----
  const listeners = new Map<string, Set<Listener>>()
  const dispatch = (event: string, data: any[]) => {
    const set = listeners.get(event)
    if (!set) return
    for (const l of Array.from(set)) {
      try {
        l.cb(...data)
      } catch (e) {
        console.error('[remoteBridge] listener error', event, e)
      }
      if (l.max > 0) {
        l.count++
        if (l.count >= l.max) set.delete(l)
      }
    }
  }

  // —— 用 fetch 轮询取代 WebSocket ——
  // Wails 的 webview 是安全上下文(wails://)，会把明文 ws:// 当混合内容拦掉(实测 0 连接)，
  // 但 http fetch 放行。所以实时推送改成定时 RPC 拉取后派发成相同事件。
  const EV_STOCK = 'market:stock:update'
  const EV_INDICES = 'market:indices:update'
  const EV_TELEGRAPH = 'market:telegraph:update'
  const EV_ORDERBOOK = 'market:orderbook:update'
  const EV_KLINE = 'market:kline:update'
  const SUB_MARKET = 'market:subscribe'
  const SUB_ORDERBOOK = 'market:orderbook:subscribe'
  const SUB_KLINE = 'market:kline:subscribe'

  let marketCodes: string[] = [] // 已订阅个股(决定个股实时轮询)
  let obCode = '' // 订阅的盘口代码
  let klineSub: { code: string; period: string } | null = null // 订阅的K线
  const seenTelegraph = new Set<string>()
  let telegraphFirst = true

  const safeRpc = async (method: string, args: any[]) => {
    try {
      return await rpc(method, args)
    } catch {
      return null
    }
  }
  const pollIndices = async () => {
    const idx = await safeRpc('GetMarketIndices', [])
    if (Array.isArray(idx) && idx.length) dispatch(EV_INDICES, [idx])
  }
  const pollStocks = async () => {
    if (!marketCodes.length) return
    const stocks = await safeRpc('GetStockRealTimeData', [marketCodes])
    if (Array.isArray(stocks) && stocks.length) dispatch(EV_STOCK, [stocks])
  }
  const telegraphKey = (t: any) =>
    String(t?.id ?? t?.ID ?? `${t?.time ?? t?.Time ?? ''}|${t?.title ?? t?.Title ?? t?.content ?? ''}`)
  const pollTelegraph = async () => {
    const list = await safeRpc('GetTelegraphList', [])
    if (!Array.isArray(list) || !list.length) return
    // 约定 list 为「新→旧」：首轮补最近 20 条，之后只补新增；倒序派发让最新置顶。
    const items = telegraphFirst ? list.slice(0, 20) : list
    for (let i = items.length - 1; i >= 0; i--) {
      const key = telegraphKey(items[i])
      if (seenTelegraph.has(key)) continue
      seenTelegraph.add(key)
      dispatch(EV_TELEGRAPH, [items[i]])
    }
    telegraphFirst = false
  }
  const pollOrderBook = async () => {
    if (!obCode) return
    const ob = await safeRpc('GetOrderBook', [obCode])
    if (ob && typeof ob === 'object') dispatch(EV_ORDERBOOK, [ob])
  }
  const klineLen = (period: string) => (period === '5d' ? 1250 : period === '1m' ? 250 : 240)
  const pollKLine = async () => {
    if (!klineSub || !klineSub.code) return
    const { code, period } = klineSub
    const data = await safeRpc('GetKLineData', [code, period, klineLen(period)])
    if (Array.isArray(data) && data.length) dispatch(EV_KLINE, [{ code, period, data }])
  }
  // 立即拉一次 + 定时轮询(盘口最快、快讯较慢)
  const kick = () => {
    pollIndices()
    pollTelegraph()
    pollStocks()
    pollOrderBook()
    pollKLine()
  }
  setTimeout(kick, 300)
  setInterval(pollIndices, 5000)
  setInterval(pollStocks, 4000)
  setInterval(pollTelegraph, 20000)
  setInterval(pollOrderBook, 2500)
  setInterval(pollKLine, 5000)

  // —— 圆桌会议消息轮询 ——
  // 专家发言由后端 rt.Emit("meeting:message:<code>") 推送，远程模式 WS 不通收不到。
  // 但消息都落了 session，所以有人监听该事件时就轮询 GetSessionMessages(code)，
  // 把新增的非用户消息派发成同名事件(用户消息前端已本地即时显示，跳过防重复)。
  const meetingPollers = new Map<string, { timer: any; count: number; primed: boolean }>()
  const ensureMeetingPoller = (code: string) => {
    if (meetingPollers.has(code)) return
    const st = { timer: 0 as any, count: 0, primed: false }
    const tick = async () => {
      const msgs = await safeRpc('GetSessionMessages', [code])
      if (!Array.isArray(msgs)) return
      if (!st.primed) {
        // 基线：首轮只记数不派发(历史消息由 AgentRoom 自己加载)
        st.count = msgs.length
        st.primed = true
        return
      }
      if (msgs.length > st.count) {
        for (const m of msgs.slice(st.count)) {
          if (m && m.agentId !== 'user') dispatch(`meeting:message:${code}`, [m])
        }
        st.count = msgs.length
      } else if (msgs.length < st.count) {
        st.count = msgs.length // 会话被清空
      }
    }
    st.timer = setInterval(tick, 2000)
    meetingPollers.set(code, st)
    tick()
  }
  const stopMeetingPoller = (code: string) => {
    const st = meetingPollers.get(code)
    if (st) {
      clearInterval(st.timer)
      meetingPollers.delete(code)
    }
  }

  const rt = (w.runtime = w.runtime || {})
  rt.EventsOnMultiple = (event: string, cb: AnyFn, maxCallbacks: number) => {
    let set = listeners.get(event)
    if (!set) {
      set = new Set()
      listeners.set(event, set)
    }
    const l: Listener = { cb, max: maxCallbacks ?? -1, count: 0 }
    set.add(l)
    if (event.startsWith('meeting:message:')) {
      ensureMeetingPoller(event.slice('meeting:message:'.length))
    }
    return () => set!.delete(l)
  }
  rt.EventsOn = (event: string, cb: AnyFn) => rt.EventsOnMultiple(event, cb, -1)
  rt.EventsOnce = (event: string, cb: AnyFn) => rt.EventsOnMultiple(event, cb, 1)
  rt.EventsOff = (event: string, ..._rest: string[]) => {
    listeners.delete(event)
    if (event.startsWith('meeting:message:')) {
      stopMeetingPoller(event.slice('meeting:message:'.length))
    }
  }
  rt.EventsEmit = (event: string, ...data: any[]) => {
    // 本地派发(若有本地监听)
    dispatch(event, data)
    // 拦截订阅事件 → 更新各自的轮询目标(不再走 WS)。
    if (event === SUB_MARKET) {
      const codes = Array.isArray(data[0]) ? data[0] : []
      marketCodes = codes.filter((c: any) => typeof c === 'string')
      pollStocks()
    } else if (event === SUB_ORDERBOOK) {
      obCode = typeof data[0] === 'string' ? data[0] : ''
      pollOrderBook()
    } else if (event === SUB_KLINE) {
      const code = typeof data[0] === 'string' ? data[0] : ''
      const period = typeof data[1] === 'string' ? data[1] : '1d'
      klineSub = code ? { code, period } : null
      pollKLine()
    }
  }

  console.info('[remoteBridge] 已启用远程后端', base)
}
