import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'path'

export default defineConfig({
  plugins: [react()],
  // __ADMIN_BUILD__:编译期常量。只有本机个人构建设 VITE_ADMIN_BUILD=1 时为 true,
  // 「账号管理」代码才会被编入;分发构建(不设该环境变量)恒为 false,Vite 把死分支连同
  // AccountsSettings 组件一起从产物中剔除——安装包里根本不含此功能。
  // **分发/线上构建绝不可设置 VITE_ADMIN_BUILD。** 失效即安全:忘了就等于"不含此功能"。
  define: {
    __ADMIN_BUILD__: JSON.stringify(process.env.VITE_ADMIN_BUILD === '1'),
  },
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
      '@wailsjs': path.resolve(__dirname, './wailsjs')
    }
  }
})
