//go:build !darwin || !cgo

package main

// disablePressAndHold is a no-op everywhere except a cgo-enabled macOS build.
// The accent chooser is a macOS-only behaviour, and the non-darwin desktop
// targets are built without an Objective-C compiler.
func disablePressAndHold() {}
