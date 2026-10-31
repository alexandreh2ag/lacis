package main

import (
	"os"

	"github.com/alexandreh2ag/lacis/cli"
	"github.com/alexandreh2ag/lacis/context"
)

func main() {
	ctx := context.DefaultContext()
	if err := cli.GetRootCmd(ctx).Execute(); err != nil {
		os.Exit(1)
	}
}
