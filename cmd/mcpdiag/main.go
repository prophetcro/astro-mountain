package main

import (
	"fmt"
	"os"
)

const exitUsage = 2

type subcommand struct {
	Name  string
	Brief string
	Run   func(args []string) error
}

var subcommands = []subcommand{
	{
		Name:  "cloud",
		Brief: "打印地表分层云量与气压层剖面的逐时对照，核实「剖面之上有云」兜底判据",
		Run:   runCloud,
	},
}

func usage(w *os.File) {
	fmt.Fprintln(w, "mcpdiag — astro-mountain 只读诊断工具（不修改任何评级逻辑）")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "用法：")
	fmt.Fprintln(w, "  mcpdiag <子命令> [参数...]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "子命令：")
	for _, sc := range subcommands {
		fmt.Fprintf(w, "  %-8s %s\n", sc.Name, sc.Brief)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "查看某个子命令的参数：")
	fmt.Fprintln(w, "  mcpdiag cloud -h")
}

func lookup(name string) *subcommand {
	for i := range subcommands {
		if subcommands[i].Name == name {
			return &subcommands[i]
		}
	}
	return nil
}

func main() {
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(exitUsage)
	}

	name := os.Args[1]
	switch name {
	case "-h", "--help", "help":
		usage(os.Stdout)
		return
	}

	sc := lookup(name)
	if sc == nil {
		fmt.Fprintf(os.Stderr, "未知子命令：%q\n\n", name)
		usage(os.Stderr)
		os.Exit(exitUsage)
	}

	if err := sc.Run(os.Args[2:]); err != nil {
		fmt.Fprintf(os.Stderr, "mcpdiag %s 失败：%v\n", name, err)
		os.Exit(1)
	}
}
