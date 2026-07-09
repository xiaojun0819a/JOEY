// makepatch 生成 NAS 自更新用的 bsdiff 增量补丁(与 go-update 的 BSDiffPatcher 兼容)。
// 用法: go run ./tools/makepatch <旧文件> <新文件> <补丁输出>
package main

import (
	"fmt"
	"os"

	"github.com/kr/binarydist"
)

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "用法: makepatch <old> <new> <patch>")
		os.Exit(2)
	}
	oldF, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer oldF.Close()
	newF, err := os.Open(os.Args[2])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer newF.Close()
	out, err := os.Create(os.Args[3])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer out.Close()
	if err := binarydist.Diff(oldF, newF, out); err != nil {
		fmt.Fprintln(os.Stderr, "diff失败:", err)
		os.Exit(1)
	}
	fmt.Println("ok:", os.Args[3])
}
