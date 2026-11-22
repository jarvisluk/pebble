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

func goStr(s *C.char) string {
	if s == nil {
		return ""
	}
	return C.GoString(s)
}

//export BothubTailscaleStart
func BothubTailscaleStart(cStateDir, cHostname, cControlURL, cAuthKey *C.char) *C.char {
	return C.CString(tailscaleStartJSON(goStr(cStateDir), goStr(cHostname), goStr(cControlURL), goStr(cAuthKey)))
}

//export BothubTailscaleStatus
func BothubTailscaleStatus(includePeers C.int) *C.char {
	return C.CString(tailscaleStatusJSON(includePeers != 0))
}

//export BothubTailscaleLogout
func BothubTailscaleLogout() *C.char {
	return C.CString(tailscaleLogoutJSON(""))
}

func main() {}
