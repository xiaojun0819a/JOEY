import React, { createContext, useContext, useState, useEffect, ReactNode } from 'react';
import { getConfig, updateConfig } from '../services/configService';

// 主题类型定义
export type ThemeType =
  | 'military' | 'ocean' | 'purple' | 'orange' | 'dark'  // 深色主题
  | 'light' | 'light-blue' | 'light-green' | 'light-rose';  // 浅色主题

interface ThemeColors {
  name: string;
  isDark: boolean;  // 是否深色主题
  bg0: string;
  bg1: string;
  panel: string;
  panelStrong: string;
  panelSoft: string;
  stroke: string;
  strokeStrong: string;
  text0: string;
  text1: string;
  text2: string;  // 更浅的文字颜色
  accent: string;
  accent2: string;
  shadow: string;  // 阴影颜色
  shadowStrong: string;  // 强阴影颜色
}

// 主题配置
export const themes: Record<ThemeType, ThemeColors> = {
  // ===== 深色主题 =====
  military: {
    name: '韭菜绿',
    isDark: true,
    bg0: '#080d08',
    bg1: '#0e1610',
    panel: 'rgba(14, 22, 16, 0.75)',
    panelStrong: 'rgba(14, 22, 16, 0.92)',
    panelSoft: 'rgba(14, 22, 16, 0.56)',
    stroke: 'rgba(58, 95, 50, 0.22)',
    strokeStrong: 'rgba(58, 95, 50, 0.32)',
    text0: '#d8e0d6',
    text1: '#7a8a72',
    text2: '#4a5a42',
    accent: '#3a5f32',
    accent2: '#5a8a4a',
    shadow: 'rgba(0, 0, 0, 0.35)',
    shadowStrong: 'rgba(0, 0, 0, 0.5)',
  },
  ocean: {
    name: '海洋蓝',
    isDark: true,
    bg0: '#0a0f1a',
    bg1: '#0f1629',
    panel: 'rgba(15, 22, 41, 0.72)',
    panelStrong: 'rgba(15, 22, 41, 0.92)',
    panelSoft: 'rgba(15, 22, 41, 0.56)',
    stroke: 'rgba(56, 189, 248, 0.18)',
    strokeStrong: 'rgba(56, 189, 248, 0.28)',
    text0: '#e2e8f0',
    text1: '#94a3b8',
    text2: '#64748b',
    accent: '#38bdf8',
    accent2: '#22d3ee',
    shadow: 'rgba(0, 0, 0, 0.35)',
    shadowStrong: 'rgba(0, 0, 0, 0.5)',
  },
  purple: {
    name: '星空紫',
    isDark: true,
    bg0: '#0f0a1a',
    bg1: '#1a1025',
    panel: 'rgba(26, 16, 37, 0.72)',
    panelStrong: 'rgba(26, 16, 37, 0.92)',
    panelSoft: 'rgba(26, 16, 37, 0.56)',
    stroke: 'rgba(168, 85, 247, 0.18)',
    strokeStrong: 'rgba(168, 85, 247, 0.28)',
    text0: '#f0e8f5',
    text1: '#a89bb8',
    text2: '#786888',
    accent: '#a855f7',
    accent2: '#c084fc',
    shadow: 'rgba(0, 0, 0, 0.35)',
    shadowStrong: 'rgba(0, 0, 0, 0.5)',
  },
  orange: {
    name: '暖橙',
    isDark: true,
    bg0: '#120d08',
    bg1: '#1f1610',
    panel: 'rgba(31, 22, 16, 0.72)',
    panelStrong: 'rgba(31, 22, 16, 0.92)',
    panelSoft: 'rgba(31, 22, 16, 0.56)',
    stroke: 'rgba(251, 146, 60, 0.18)',
    strokeStrong: 'rgba(251, 146, 60, 0.28)',
    text0: '#f5ebe0',
    text1: '#b8a08b',
    text2: '#88705b',
    accent: '#fb923c',
    accent2: '#fdba74',
    shadow: 'rgba(0, 0, 0, 0.35)',
    shadowStrong: 'rgba(0, 0, 0, 0.5)',
  },
  dark: {
    name: '暗夜黑',
    isDark: true,
    // 纯黑基调,与K线/分时图表底色(#000000)完全一致;面板留极轻灰阶保住卡片层次
    bg0: '#000000',
    bg1: '#0a0a0c',
    panel: 'rgba(10, 10, 12, 0.72)',
    panelStrong: 'rgba(10, 10, 12, 0.92)',
    panelSoft: 'rgba(10, 10, 12, 0.56)',
    stroke: 'rgba(161, 161, 170, 0.18)',
    strokeStrong: 'rgba(161, 161, 170, 0.28)',
    text0: '#fafafa',
    text1: '#a1a1aa',
    text2: '#71717a',
    accent: '#a1a1aa',
    accent2: '#d4d4d8',
    shadow: 'rgba(0, 0, 0, 0.35)',
    shadowStrong: 'rgba(0, 0, 0, 0.5)',
  },

  // ===== 浅色主题 =====
  light: {
    name: '经典白',
    isDark: false,
    bg0: '#f8fafc',
    bg1: '#f1f5f9',
    panel: 'rgba(255, 255, 255, 0.85)',
    panelStrong: 'rgba(255, 255, 255, 0.95)',
    panelSoft: 'rgba(255, 255, 255, 0.65)',
    stroke: 'rgba(148, 163, 184, 0.3)',
    strokeStrong: 'rgba(148, 163, 184, 0.5)',
    text0: '#1e293b',
    text1: '#64748b',
    text2: '#94a3b8',
    accent: '#475569',
    accent2: '#334155',
    shadow: 'rgba(100, 116, 139, 0.12)',
    shadowStrong: 'rgba(100, 116, 139, 0.2)',
  },
  'light-blue': {
    name: '天空蓝',
    isDark: false,
    bg0: '#f0f9ff',
    bg1: '#e0f2fe',
    panel: 'rgba(255, 255, 255, 0.85)',
    panelStrong: 'rgba(255, 255, 255, 0.95)',
    panelSoft: 'rgba(240, 249, 255, 0.65)',
    stroke: 'rgba(14, 165, 233, 0.25)',
    strokeStrong: 'rgba(14, 165, 233, 0.4)',
    text0: '#0c4a6e',
    text1: '#0369a1',
    text2: '#0ea5e9',
    accent: '#0ea5e9',
    accent2: '#0284c7',
    shadow: 'rgba(14, 165, 233, 0.1)',
    shadowStrong: 'rgba(14, 165, 233, 0.18)',
  },
  'light-green': {
    name: '薄荷绿',
    isDark: false,
    bg0: '#f0fdf4',
    bg1: '#dcfce7',
    panel: 'rgba(255, 255, 255, 0.85)',
    panelStrong: 'rgba(255, 255, 255, 0.95)',
    panelSoft: 'rgba(240, 253, 244, 0.65)',
    stroke: 'rgba(34, 197, 94, 0.25)',
    strokeStrong: 'rgba(34, 197, 94, 0.4)',
    text0: '#14532d',
    text1: '#166534',
    text2: '#22c55e',
    accent: '#22c55e',
    accent2: '#16a34a',
    shadow: 'rgba(34, 197, 94, 0.1)',
    shadowStrong: 'rgba(34, 197, 94, 0.18)',
  },
  'light-rose': {
    name: '樱花粉',
    isDark: false,
    bg0: '#fff1f2',
    bg1: '#ffe4e6',
    panel: 'rgba(255, 255, 255, 0.85)',
    panelStrong: 'rgba(255, 255, 255, 0.95)',
    panelSoft: 'rgba(255, 241, 242, 0.65)',
    stroke: 'rgba(244, 63, 94, 0.2)',
    strokeStrong: 'rgba(244, 63, 94, 0.35)',
    text0: '#881337',
    text1: '#be123c',
    text2: '#f43f5e',
    accent: '#f43f5e',
    accent2: '#e11d48',
    shadow: 'rgba(244, 63, 94, 0.1)',
    shadowStrong: 'rgba(244, 63, 94, 0.18)',
  },
};

interface ThemeContextType {
  theme: ThemeType;
  setTheme: (theme: ThemeType) => void;
  colors: ThemeColors;
}

const ThemeContext = createContext<ThemeContextType | undefined>(undefined);

export const ThemeProvider: React.FC<{ children: ReactNode }> = ({ children }) => {
  const [theme, setThemeState] = useState<ThemeType>('military');

  // 从 config 加载主题
  useEffect(() => {
    getConfig().then((config) => {
      const savedTheme = config.theme as ThemeType;
      if (savedTheme && themes[savedTheme]) {
        setThemeState(savedTheme);
      }
    }).catch(() => {
      // 使用默认主题
    });
  }, []);

  const setTheme = async (newTheme: ThemeType) => {
    setThemeState(newTheme);
    try {
      const config = await getConfig();
      config.theme = newTheme;
      await updateConfig(config);
    } catch (e) {
      console.error('Failed to save theme:', e);
    }
  };

  // 应用主题到 CSS 变量
  useEffect(() => {
    const colors = themes[theme];
    const root = document.documentElement;

    root.style.setProperty('--bg-0', colors.bg0);
    root.style.setProperty('--bg-1', colors.bg1);
    root.style.setProperty('--panel', colors.panel);
    root.style.setProperty('--panel-strong', colors.panelStrong);
    root.style.setProperty('--panel-soft', colors.panelSoft);
    root.style.setProperty('--stroke', colors.stroke);
    root.style.setProperty('--stroke-strong', colors.strokeStrong);
    root.style.setProperty('--text-0', colors.text0);
    root.style.setProperty('--text-1', colors.text1);
    root.style.setProperty('--text-2', colors.text2);
    root.style.setProperty('--accent', colors.accent);
    root.style.setProperty('--accent-2', colors.accent2);
    root.style.setProperty('--shadow', colors.shadow);
    root.style.setProperty('--shadow-strong', colors.shadowStrong);
    root.style.setProperty('--is-dark', colors.isDark ? '1' : '0');

    // 设置 data-theme 属性用于 CSS 选择器
    root.setAttribute('data-theme', theme);
    root.setAttribute('data-theme-mode', colors.isDark ? 'dark' : 'light');
  }, [theme]);

  return (
    <ThemeContext.Provider value={{ theme, setTheme, colors: themes[theme] }}>
      {children}
    </ThemeContext.Provider>
  );
};

export const useTheme = () => {
  const context = useContext(ThemeContext);
  if (!context) {
    throw new Error('useTheme must be used within a ThemeProvider');
  }
  return context;
};
