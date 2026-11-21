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
import "unsafe"

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
