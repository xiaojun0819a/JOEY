import React from 'react'
import {createRoot} from 'react-dom/client'
import './style.css'
import App from './App'
import SafeBoundary from './components/SafeBoundary'
import { ThemeProvider } from './contexts/ThemeContext'
import { CandleColorProvider } from './contexts/CandleColorContext'
import { IndicatorProvider } from './contexts/IndicatorContext'
import { resolveBackendMode, installRemoteBridge, showOfflineBanner } from './services/remoteBridge'

const hideWailsSpinner = () => {
    const spinner = document.getElementById('wails-spinner')
    if (spinner) {
        spinner.style.display = 'none'
        spinner.style.visibility = 'hidden'
    }
}

const showFatalBootError = (message: string) => {
    const root = document.getElementById('root')
    if (!root) return
    root.innerHTML = `
      <div style="height:100vh;display:flex;align-items:center;justify-content:center;background:#0b0f17;color:#f8fafc;font-family:system-ui,-apple-system,Segoe UI,Roboto,sans-serif;">
        <div style="max-width:560px;padding:16px 18px;border:1px solid rgba(248,113,113,.45);background:rgba(127,29,29,.32);border-radius:10px;font-size:14px;line-height:1.5;">
          <div style="font-weight:700;margin-bottom:6px;">页面启动异常</div>
          <div>${message.replace(/</g, '&lt;')}</div>
        </div>
      </div>
    `
}

window.addEventListener('error', (event) => {
    console.error('[boot:error]', event.error || event.message)
    hideWailsSpinner()
})

window.addEventListener('unhandledrejection', (event) => {
    console.error('[boot:unhandledrejection]', event.reason)
    hideWailsSpinner()
})

hideWailsSpinner()

// 禁用 WebView 的"页面缩放"(Ctrl+滚轮 / Ctrl加减 / 触控板双指捏合会以浏览器方式缩放整页,布局会乱、变丑)。
// 窗口大小改变靠拖边框(响应式布局等比缩放)。图表自身的缩放用的是不带 Ctrl 的普通滚轮,不受影响。
window.addEventListener(
    'wheel',
    (e) => {
        if (e.ctrlKey) e.preventDefault() // 触控板捏合在浏览器里也表现为 ctrlKey 的 wheel
    },
    { passive: false }
)
window.addEventListener('keydown', (e) => {
    if ((e.ctrlKey || e.metaKey) && ['+', '-', '=', '_', '0'].includes(e.key)) e.preventDefault()
})

const container = document.getElementById('root')

if (!container) {
    showFatalBootError('找不到根节点 #root')
    throw new Error('Root container missing')
}

const renderApp = () => {
    try {
        const root = createRoot(container)
        root.render(
            <React.StrictMode>
                <ThemeProvider>
                    <CandleColorProvider>
                        <IndicatorProvider>
                            <SafeBoundary title="页面渲染异常">
                                <App/>
                            </SafeBoundary>
                        </IndicatorProvider>
                    </CandleColorProvider>
                </ThemeProvider>
            </React.StrictMode>
        )
    } catch (error: any) {
        const text = error?.message || '未知前端启动错误'
        showFatalBootError(text)
    }
}

// 启动前先决定后端模式：探到 NAS 可达则装远程桥接(RPC/WS 改道 NAS)，否则用内置绑定。
// 无论如何都要渲染，探测失败/超时一律回落本地。
;(async () => {
    try {
        const backend = await resolveBackendMode()
        if (backend.mode === 'remote' && backend.url) {
            installRemoteBridge(backend.url, backend.token)
            console.info('[boot] 后端模式: remote →', backend.url)
        } else if (backend.mode === 'fallback') {
            // 配了 NAS 但没连上：用本地旧数据。横幅只给主人(配置里有令牌)看,
            // 访客(分发版)不显示技术性提示,避免困扰普通用户
            console.warn('[boot] 后端模式: fallback(NAS 未连接) →', backend.url)
            if (backend.token) showOfflineBanner(backend.url || '')
        } else {
            console.info('[boot] 后端模式: local')
        }
    } catch (e) {
        console.warn('[boot] 后端探测异常，回落本地', e)
    } finally {
        renderApp()
    }
})()

setTimeout(hideWailsSpinner, 200)
