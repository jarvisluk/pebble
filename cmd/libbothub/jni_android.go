//go:build android

package main

/*
#include <jni.h>
#include <stdlib.h>

static const char* jstrToCStr(JNIEnv* env, jstring s) {
	if (s == NULL) return NULL;
	return (*env)->GetStringUTFChars(env, s, NULL);
}
static void releaseJStr(JNIEnv* env, jstring s, const char* cs) {
	if (s != NULL && cs != NULL) (*env)->ReleaseStringUTFChars(env, s, cs);
}
static jstring cStrToJStr(JNIEnv* env, const char* cs) {
	if (cs == NULL) return NULL;
	return (*env)->NewStringUTF(env, cs);
}
*/
import "C"
import (
	"net"
	"net/netip"
	"time"
	"unsafe"

	"tailscale.com/net/netmon"
)

func init() {
	netmon.RegisterInterfaceGetter(androidInterfaces)
}

func androidInterfaces() ([]netmon.Interface, error) {
	var addrs []net.Addr
	if ip, ok := androidOutboundIP("udp4", "8.8.8.8:53"); ok {
		addrs = append(addrs, &net.IPNet{IP: ip.AsSlice(), Mask: net.CIDRMask(32, 32)})
	}
	if ip, ok := androidOutboundIP("udp6", "[2001:4860:4860::8888]:53"); ok {
		addrs = append(addrs, &net.IPNet{IP: ip.AsSlice(), Mask: net.CIDRMask(128, 128)})
	}
	if len(addrs) == 0 {
		addrs = []net.Addr{}
	}
	return []netmon.Interface{{
		Interface: &net.Interface{
			Index: 1,
			MTU:   1500,
			Name:  "android",
			Flags: net.FlagUp | net.FlagRunning,
		},
		AltAddrs: addrs,
	}}, nil
}

func androidOutboundIP(network, addr string) (netip.Addr, bool) {
	d := net.Dialer{Timeout: 2 * time.Second}
	c, err := d.Dial(network, addr)
	if err != nil {
		return netip.Addr{}, false
	}
	defer c.Close()
	ua, ok := c.LocalAddr().(*net.UDPAddr)
	if !ok {
		return netip.Addr{}, false
	}
	ip, ok := netip.AddrFromSlice(ua.IP)
	if !ok {
		return netip.Addr{}, false
	}
	ip = ip.Unmap()
	if ip.IsLoopback() || ip.IsUnspecified() {
		return netip.Addr{}, false
	}
	return ip, true
}

//export Java_bothub_tunnel_BothubTunnelModule_nativeStartTunnel
func Java_bothub_tunnel_BothubTunnelModule_nativeStartTunnel(env *C.JNIEnv, obj C.jobject, jLink C.jstring, jToken C.jstring) C.jstring {
	cLink := C.jstrToCStr(env, jLink)
	link := ""
	if cLink != nil {
		link = C.GoString(cLink)
		C.releaseJStr(env, jLink, cLink)
	}
	cTok := C.jstrToCStr(env, jToken)
	tok := ""
	if cTok != nil {
		tok = C.GoString(cTok)
		C.releaseJStr(env, jToken, cTok)
	}
	res := startTunnelJSON(link, tok)
	cs := C.CString(res)
	defer C.free(unsafe.Pointer(cs))
	return C.cStrToJStr(env, cs)
}

//export Java_bothub_tunnel_BothubTunnelModule_nativeStopTunnel
func Java_bothub_tunnel_BothubTunnelModule_nativeStopTunnel(env *C.JNIEnv, obj C.jobject) {
	stopTunnelInternal()
}

func jniStr(env *C.JNIEnv, js C.jstring) string {
	cs := C.jstrToCStr(env, js)
	if cs == nil {
		return ""
	}
	s := C.GoString(cs)
	C.releaseJStr(env, js, cs)
	return s
}

func retJniStr(env *C.JNIEnv, s string) C.jstring {
	cs := C.CString(s)
	defer C.free(unsafe.Pointer(cs))
	return C.cStrToJStr(env, cs)
}

//export Java_bothub_tunnel_BothubTunnelModule_nativeTailscaleStart
func Java_bothub_tunnel_BothubTunnelModule_nativeTailscaleStart(env *C.JNIEnv, obj C.jobject, jStateDir, jHostname, jControlURL, jAuthKey C.jstring) C.jstring {
	return retJniStr(env, tailscaleStartJSON(jniStr(env, jStateDir), jniStr(env, jHostname), jniStr(env, jControlURL), jniStr(env, jAuthKey)))
}

//export Java_bothub_tunnel_BothubTunnelModule_nativeTailscaleStatus
func Java_bothub_tunnel_BothubTunnelModule_nativeTailscaleStatus(env *C.JNIEnv, obj C.jobject, includePeers C.jboolean) C.jstring {
	return retJniStr(env, tailscaleStatusJSON(includePeers != 0))
}

//export Java_bothub_tunnel_BothubTunnelModule_nativeTailscaleLogout
func Java_bothub_tunnel_BothubTunnelModule_nativeTailscaleLogout(env *C.JNIEnv, obj C.jobject) C.jstring {
	return retJniStr(env, tailscaleLogoutJSON(""))
}
