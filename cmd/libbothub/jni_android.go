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
	"unsafe"

	"github.com/wlynxg/anet"
	"tailscale.com/net/netmon"
)

func init() {
	netmon.RegisterInterfaceGetter(func() ([]netmon.Interface, error) {
		ifs, err := anet.Interfaces()
		if err != nil {
			return nil, err
		}
		ret := make([]netmon.Interface, 0, len(ifs))
		for i := range ifs {
			ifi := ifs[i]
			addrs, _ := anet.InterfaceAddrsByInterface(&ifi)
			if addrs == nil {
				addrs = []net.Addr{}
			}
			ret = append(ret, netmon.Interface{
				Interface: &ifi,
				AltAddrs:  addrs,
			})
		}
		return ret, nil
	})
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
