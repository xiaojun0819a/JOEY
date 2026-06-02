import React from 'react';

interface SafeBoundaryProps {
  title?: string;
  resetKey?: string | number;
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

  render() {
    if (this.state.hasError) {
      return (
        <div className="mx-2 my-2 rounded border border-red-500/30 bg-red-500/10 px-3 py-2 text-xs text-red-300">
          {this.props.title || '模块渲染异常'}，请切换周期或股票重试。
        </div>
      );
    }
    return this.props.children;
  }
}

export default SafeBoundary;
