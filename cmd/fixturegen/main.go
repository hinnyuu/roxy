package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	out := flag.String("out", "", "输出根目录（如 test_share/library），必填")
	flag.Parse()
	if *out == "" {
		fmt.Fprintln(os.Stderr, "用法: fixturegen -out <目录>")
		os.Exit(2)
	}
	if err := Generate(*out); err != nil {
		fmt.Fprintln(os.Stderr, "生成失败:", err)
		os.Exit(1)
	}
	printTree(*out)
	fmt.Printf("\n完成：假库已生成于 %s（%d 个文件）\n", *out, len(specs()))
}
