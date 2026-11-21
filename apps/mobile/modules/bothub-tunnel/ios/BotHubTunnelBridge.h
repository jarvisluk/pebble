#ifndef BOTHUB_TUNNEL_BRIDGE_H
#define BOTHUB_TUNNEL_BRIDGE_H

#ifdef __cplusplus
extern "C" {
#endif

extern char* BothubStartTunnel(char* link, char* token);
extern void BothubStopTunnel(void);
extern void BothubFreeString(char* p);

#ifdef __cplusplus
}
#endif

#endif
