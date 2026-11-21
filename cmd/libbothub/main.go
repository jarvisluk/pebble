package main

/*
#include <stdlib.h>
*/
import "C"
import "unsafe"

//export BothubStartTunnel
func BothubStartTunnel(cLink *C.char, cToken *C.char) *C.char {
	link := ""
	if cLink != nil {
		link = C.GoString(cLink)
	}
	tok := ""
	if cToken != nil {
		tok = C.GoString(cToken)
	}
	return C.CString(startTunnelJSON(link, tok))
}

//export BothubStopTunnel
func BothubStopTunnel() {
	stopTunnelInternal()
}

//export BothubFreeString
func BothubFreeString(s *C.char) {
	if s != nil {
		C.free(unsafe.Pointer(s))
	}
}

func main() {}
