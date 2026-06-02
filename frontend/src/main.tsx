import React from 'react'
import {createRoot} from 'react-dom/client'
import './style.css'
import App from './App'
import SafeBoundary from './components/SafeBoundary'
import { ThemeProvider } from './contexts/ThemeContext'
import { CandleColorProvider } from './contexts/CandleColorContext'
import { IndicatorProvider } from './contexts/IndicatorContext'

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

const container = document.getElementById('root')

if (!container) {
    showFatalBootError('找不到根节点 #root')
    throw new Error('Root container missing')
}

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

setTimeout(hideWailsSpinner, 200)
