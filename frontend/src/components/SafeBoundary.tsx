import React from 'react';

interface SafeBoundaryProps {
  title?: string;
  resetKey?: string | number;
  onReset?: () => void; // 可选：点「返回」时额外做的事(如关掉出错的弹窗)
  children: React.ReactNode;
}

interface SafeBoundaryState {
  hasError: boolean;
}

export class SafeBoundary extends React.Component<SafeBoundaryProps, SafeBoundaryState> {
  state: SafeBoundaryState = { hasError: false };

  static getDerivedStateFromError(): SafeBoundaryState {
    return { hasError: true };
  }

  componentDidCatch(error: Error, info: React.ErrorInfo) {
    // Keep the app alive on render crashes while still surfacing details for debugging.
    console.error('[SafeBoundary] render failed:', error, info);
  }

  componentDidUpdate(prevProps: SafeBoundaryProps) {
    if (this.state.hasError && prevProps.resetKey !== this.props.resetKey) {
      this.setState({ hasError: false });
    }
  }

  private handleRetry = () => {
    // 软重试:清掉错误态,重新渲染子树。若崩溃是确定性的会立刻再崩,此时用「重新加载」。
    this.setState({ hasError: false });
    try { this.props.onReset?.(); } catch { /* ignore */ }
  };

  private handleReload = () => {
    // 硬恢复:重载 webview,一定能回到正常界面(出错的弹窗/页面会被重置关闭)。
    window.location.reload();
  };

  render() {
    if (this.state.hasError) {
      const btn =
        'px-3 py-1.5 rounded-md text-xs font-medium cursor-pointer border transition-colors';
      return (
        <div className="flex flex-col items-center justify-center gap-3 mx-2 my-6 rounded-lg border border-red-500/30 bg-red-500/10 px-4 py-6 text-center">
          <div className="text-sm text-red-300">
            {this.props.title || '模块渲染异常'}
          </div>
          <div className="text-xs text-red-300/70">
            这个页面加载出错了。可以点「返回」回到上一步,或「重新加载」刷新整个界面。
          </div>
          <div className="flex items-center gap-2 mt-1">
            <button
              onClick={this.handleRetry}
              className={`${btn} border-red-400/40 text-red-200 hover:bg-red-500/20`}
            >
              返回 / 重试
            </button>
            <button
              onClick={this.handleReload}
              className={`${btn} border-sky-400/50 bg-sky-500/20 text-sky-100 hover:bg-sky-500/30`}
            >
              重新加载
            </button>
          </div>
        </div>
      );
    }
    return this.props.children;
  }
}

export default SafeBoundary;
