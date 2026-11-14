//go:build !darwin

package main

import (
	"errors"

	memory "github.com/prithivrajmu/elephant"
)

// menuBarCommand is the internal command that runs the shared per-user host.
const menuBarCommand = "_menu-bar"

// serveWithMenuBar has no menu bar outside macOS; serve runs unchanged.
func serveWithMenuBar(svc memory.Service, _ string, serve func(memory.Service) error) error {
	return serve(svc)
}

func runMenuBarHost() error {
	return errors.New("elephant menu bar is supported only on macOS")
}
