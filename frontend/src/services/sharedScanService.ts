// 策略扫描结果跨账号共享:任一账号扫完,后端进共享池;弹窗打开即取最新显示(不重扫),
// 打开期间每10秒轮询,谁点了重新扫描,所有账号的弹窗自动换成最新结果。
// 本地模式(window.go 为真 Wails 绑定,无 __sharedResult)自动降级为"无共享",不影响单机使用。

export interface SharedScanHit<T = unknown> {
  found: boolean;
  at?: string; // "YYYY-MM-DD HH:mm:ss"(服务器时间)
  user?: string; // 扫描发起账号
  result?: T;
}

export const fetchSharedResult = async <T = unknown>(method: string): Promise<SharedScanHit<T>> => {
  try {
    const app = (window as unknown as { go?: { main?: { App?: Record<string, unknown> } } }).go?.main?.App;
    const fn = app?.['__sharedResult'];
    if (typeof fn !== 'function') return { found: false };
    const r = (await (fn as (m: string) => Promise<SharedScanHit<T>>)(method)) as SharedScanHit<T>;
    return r && r.found ? r : { found: false };
  } catch {
    return { found: false };
  }
};

/** 弹窗打开期间轮询共享结果:发现比 lastAt 新的就回调。返回停止函数。 */
export const watchSharedResult = <T = unknown>(
  method: string,
  onNewer: (hit: SharedScanHit<T> & { at: string }) => void,
  getLastAt: () => string,
  intervalMs = 10000,
): (() => void) => {
  let stopped = false;
  const tick = async () => {
    const hit = await fetchSharedResult<T>(method);
    if (stopped || !hit.found || !hit.at) return;
    if (hit.at > (getLastAt() || '')) onNewer(hit as SharedScanHit<T> & { at: string });
  };
  void tick();
  const iv = window.setInterval(() => void tick(), intervalMs);
  return () => { stopped = true; window.clearInterval(iv); };
};
