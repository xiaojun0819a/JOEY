// 把前端的关键路径/异常打到**后端日志**。
//
// 为什么不用 window.alert 或 console:Wails 产品版打不开 devtools,
// 而 macOS 的 WKWebView 在宿主没实现 JS 对话框代理时 **alert() 是空操作** ——
// 两条路都看不见东西,遇到问题只能靠猜(减仓按钮那次连猜三轮)。
// 后端日志这条通道是验证过能用的(SafeBoundary 一直在用)。
export function reportToBackend(where: string, message: string, detail = ''): void {
  try {
    const app = (window as unknown as { go?: { main?: { App?: Record<string, unknown> } } })?.go?.main?.App;
    const fn = app?.ReportFrontendError as undefined | ((w: string, m: string, s: string) => Promise<unknown>);
    void fn?.(where, message, detail);
  } catch { /* 上报失败就算了,绝不能反过来影响主流程 */ }
}
