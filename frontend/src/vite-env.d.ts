/// <reference types="vite/client" />

// __ADMIN_BUILD__:编译期常量,由 vite.config.ts 的 define 注入(个人版=true,分发版=false)。
// 必须在此 .d.ts 里声明(而非在 .tsx 内 `declare const`)——否则 esbuild 会把它当作局部绑定
// 而拒绝做 define 替换,导致它变成 undefined、账号管理被误剔除。
declare const __ADMIN_BUILD__: boolean;
