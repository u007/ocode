//go:build darwin && cgo

package main

/*
#cgo LDFLAGS: -framework Foundation
void ocode_disablePressAndHold(void);
*/
import "C"

// disablePressAndHold turns off macOS's "press and hold for accent characters"
// behaviour for this application so held keys repeat instead of opening the
// accent chooser. Implemented in native_darwin.m; see that file for why this
// cannot be done from the web layer. No-op via native_stub.go on every other
// platform (and on darwin builds without cgo).
func disablePressAndHold() {
	C.ocode_disablePressAndHold()
}
