//go:build !darwin || !cgo

package main

import "github.com/wailsapp/wails/v3/pkg/application"

// disablePressAndHold is a no-op everywhere except a cgo-enabled macOS build.
// The accent chooser is a macOS-only behaviour, and the non-darwin desktop
// targets are built without an Objective-C compiler.
func disablePressAndHold() {}

// printWindow uses Wails' own print everywhere except a cgo-enabled macOS build,
// where native_darwin.go uses the user's print settings instead of Wails' landscape.
func printWindow(w application.Window) error { return w.Print() }
