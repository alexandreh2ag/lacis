package main

import (
	"os"

	"github.com/alexandreh2ag/lacis/agent/cli"
	"github.com/alexandreh2ag/lacis/agent/context"
)

func main() {
	ctx := context.DefaultContext()
	if err := cli.GetRootCmd(ctx).Execute(); err != nil {
		os.Exit(1)
	}
}
