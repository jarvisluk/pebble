#ifndef BOTHUB_TUNNEL_BRIDGE_H
#define BOTHUB_TUNNEL_BRIDGE_H

#ifdef __cplusplus
extern "C" {
#endif

extern char* BothubStartTunnel(char* link, char* token);
extern void BothubStopTunnel(void);
extern void BothubFreeString(char* p);
extern char* BothubTailscaleStart(char* stateDir, char* hostname, char* controlUrl, char* authKey);
extern char* BothubTailscaleStatus(int includePeers);
extern char* BothubTailscaleLogout(void);

#ifdef __cplusplus
}
#endif

#endif
