package main

// Version 版本号，通过 ldflags 注入。放在无构建标签的共享文件里，
// 让桌面版(main.go)和 headless 版(main_headless.go)都能引用。
var Version = "dev"
