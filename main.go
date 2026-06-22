// SPDX-License-Identifier: MIT

package main

import "github.com/R0X4R/nomore403/cmd"

// Version and BuildDate are set via ldflags at build time.
var (
	Version   = "dev"
	BuildDate = "unknown"
)

func main() {
	cmd.SetVersionInfo(Version, BuildDate)
	cmd.Execute()
}
