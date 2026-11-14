//go:build !darwin

// Command onair is On Air, a menu-bar recorder for macOS.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "On Air runs on macOS.")
	os.Exit(1)
}
