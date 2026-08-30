package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	out := flag.String("out", "", "输出根目录（如 test_share/library），必填")
	probe := flag.Bool("probe", false, "只生成 M1.5 探针库（Jellyfin 多版本合并变体）")
	flag.Parse()
	if *out == "" {
		fmt.Fprintln(os.Stderr, "用法: fixturegen -out <目录> [-probe]")
		os.Exit(2)
	}
	if err := Generate(*out, *probe); err != nil {
		fmt.Fprintln(os.Stderr, "生成失败:", err)
		os.Exit(1)
	}
	list := specs()
	if *probe {
		list = probeSpecs()
	}
	printTree(*out, list)
	fmt.Printf("\n完成：已生成于 %s（%d 个文件）\n", *out, len(list))
}
