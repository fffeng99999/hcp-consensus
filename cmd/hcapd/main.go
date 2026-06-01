// hcapd 是 HCP 共识节点的守护进程入口。
// 负责初始化 Cosmos SDK 应用并启动节点服务。
package main

import (
	"fmt"
	"os"

	svrcmd "github.com/cosmos/cosmos-sdk/server/cmd"
	"github.com/fffeng99999/hcap-consensus/app"
)

func main() {
	// 创建根命令
	rootCmd := app.NewRootCmd()
	// 执行命令，失败时输出错误并退出
	if err := svrcmd.Execute(rootCmd, "", app.DefaultNodeHome); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
