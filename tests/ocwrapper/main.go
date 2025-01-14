package main

import (
	"ocwrapper/cmd"
	"ocwrapper/common"
)

func main() {
	cmd.Execute()

	common.Wg.Wait()
}
