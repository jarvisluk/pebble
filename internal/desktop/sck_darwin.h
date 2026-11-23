#ifndef BH_SCK_DARWIN_H
#define BH_SCK_DARWIN_H

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

void *bh_sck_start(uintptr_t handle, int width, int height, int fps, int bitrateKbps, int isHEVC, char **errOut);
void bh_sck_set_bitrate(void *ctx, int bitrateKbps);
void bh_sck_force_keyframe(void *ctx);
void bh_sck_stop(void *ctx);

#ifdef __cplusplus
}
#endif

#endif
