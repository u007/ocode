// Package broker contains authenticated loopback transport and shared
// language-server session primitives.
//
// Session implements compatibility-based client registration, canonical
// document state, serialized changes, reference-counted closes, diagnostic
// replay/clearing, and designated request ownership. ServeSessionRPC applies
// these checks to document and diagnostic notifications. Manager's shared
// gopls policy remains gated: automatic broker discovery and full
// server-originated request routing are not enabled until the remaining
// protocol is implemented.
package broker
