const warnedKeys = new Set<string>();

const getWindowRef = (): Window | undefined => (
  typeof window === 'undefined' ? undefined : window
);

export const isWailsGoReady = (): boolean => {
  const win = getWindowRef() as any;
  return Boolean(win?.go?.main?.App);
};

export const isWailsRuntimeReady = (): boolean => {
  const win = getWindowRef() as any;
  return Boolean(win?.runtime);
};

export const isWailsBridgeReady = (): boolean => (
  isWailsGoReady() && isWailsRuntimeReady()
);

export const warnWailsUnavailable = (
  feature: string,
  required: 'go' | 'runtime' | 'both' = 'both',
): void => {
  const key = `${feature}:${required}`;
  if (warnedKeys.has(key)) return;
  warnedKeys.add(key);
  const target = required === 'both' ? 'go+runtime' : required;
  console.info(`[wails] ${feature} 已降级：缺少 ${target} 桥接（当前为浏览器预览模式）`);
};
