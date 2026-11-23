//go:build darwin && cgo

#import <Foundation/Foundation.h>
#import <ScreenCaptureKit/ScreenCaptureKit.h>
#import <VideoToolbox/VideoToolbox.h>
#import <CoreMedia/CoreMedia.h>
#import <CoreVideo/CoreVideo.h>
#import <CoreGraphics/CoreGraphics.h>
#include <stdatomic.h>
#include <stdlib.h>
#include <string.h>
#include "sck_darwin.h"

extern void goSCKOnFrame(uintptr_t handle, void *data, int len, int isKey);

static void bh_set_err(char **errOut, NSString *msg) {
    if (!errOut) return;
    const char *utf8 = msg.UTF8String ?: "unknown ScreenCaptureKit error";
    *errOut = strdup(utf8);
}

@interface BHScreenCapturer : NSObject <SCStreamOutput, SCStreamDelegate> {
@public
    atomic_bool _forceKey;
    atomic_bool _stopped;
}
@property (nonatomic, assign) uintptr_t goHandle;
@property (nonatomic, strong) SCStream *stream;
@property (nonatomic, assign) VTCompressionSessionRef vtSession;
@property (nonatomic, strong) dispatch_queue_t sampleQueue;
@property (nonatomic, assign) int fps;
@property (nonatomic, assign) int isHEVC;
@end

static void bh_vt_output_callback(
    void *outputCallbackRefCon,
    void *sourceFrameRefCon,
    OSStatus status,
    VTEncodeInfoFlags infoFlags,
    CMSampleBufferRef sampleBuffer
) {
    (void)sourceFrameRefCon;
    (void)infoFlags;
    if (status != noErr || !sampleBuffer || !CMSampleBufferDataIsReady(sampleBuffer)) {
        return;
    }
    BHScreenCapturer *cap = (__bridge BHScreenCapturer *)outputCallbackRefCon;
    if (!cap || atomic_load(&cap->_stopped)) {
        return;
    }

    BOOL isKey = YES;
    CFArrayRef attachments = CMSampleBufferGetSampleAttachmentsArray(sampleBuffer, false);
    if (attachments && CFArrayGetCount(attachments) > 0) {
        CFDictionaryRef dict = (CFDictionaryRef)CFArrayGetValueAtIndex(attachments, 0);
        if (dict && CFDictionaryContainsKey(dict, kCMSampleAttachmentKey_NotSync)) {
            CFBooleanRef notSync = (CFBooleanRef)CFDictionaryGetValue(dict, kCMSampleAttachmentKey_NotSync);
            if (notSync && CFBooleanGetValue(notSync)) {
                isKey = NO;
            }
        }
    }

    CMBlockBufferRef dataBuffer = CMSampleBufferGetDataBuffer(sampleBuffer);
    if (!dataBuffer) return;

    size_t totalLength = 0;
    char *dataPointer = NULL;
    if (CMBlockBufferGetDataPointer(dataBuffer, 0, NULL, &totalLength, &dataPointer) != kCMBlockBufferNoErr || !dataPointer || totalLength == 0) {
        return;
    }

    NSMutableData *annexB = [[NSMutableData alloc] initWithCapacity:totalLength + 256];
    static const uint8_t startCode[4] = {0x00, 0x00, 0x00, 0x01};

    if (isKey) {
        CMFormatDescriptionRef fmt = CMSampleBufferGetFormatDescription(sampleBuffer);
        if (fmt) {
            size_t paramCount = 0;
            if (cap.isHEVC) {
                if (CMVideoFormatDescriptionGetHEVCParameterSetAtIndex(fmt, 0, NULL, NULL, &paramCount, NULL) == noErr) {
                    for (size_t i = 0; i < paramCount; i++) {
                        const uint8_t *ps = NULL;
                        size_t psSize = 0;
                        if (CMVideoFormatDescriptionGetHEVCParameterSetAtIndex(fmt, i, &ps, &psSize, NULL, NULL) == noErr && ps && psSize > 0) {
                            [annexB appendBytes:startCode length:4];
                            [annexB appendBytes:ps length:psSize];
                        }
                    }
                }
            } else {
                if (CMVideoFormatDescriptionGetH264ParameterSetAtIndex(fmt, 0, NULL, NULL, &paramCount, NULL) == noErr) {
                    for (size_t i = 0; i < paramCount; i++) {
                        const uint8_t *ps = NULL;
                        size_t psSize = 0;
                        if (CMVideoFormatDescriptionGetH264ParameterSetAtIndex(fmt, i, &ps, &psSize, NULL, NULL) == noErr && ps && psSize > 0) {
                            [annexB appendBytes:startCode length:4];
                            [annexB appendBytes:ps length:psSize];
                        }
                    }
                }
            }
        }
    }

    size_t offset = 0;
    while (offset + 4 <= totalLength) {
        uint32_t nalLen = 0;
        memcpy(&nalLen, dataPointer + offset, 4);
        nalLen = CFSwapInt32BigToHost(nalLen);
        offset += 4;
        if (nalLen == 0 || offset + nalLen > totalLength) {
            break;
        }
        [annexB appendBytes:startCode length:4];
        [annexB appendBytes:(dataPointer + offset) length:nalLen];
        offset += nalLen;
    }

    if (annexB.length > 0 && !atomic_load(&cap->_stopped)) {
        goSCKOnFrame(cap.goHandle, (void *)annexB.bytes, (int)annexB.length, isKey ? 1 : 0);
    }
}

@implementation BHScreenCapturer

- (void)stream:(SCStream *)stream didOutputSampleBuffer:(CMSampleBufferRef)sampleBuffer ofType:(SCStreamOutputType)type {
    (void)stream;
    if (type != SCStreamOutputTypeScreen || atomic_load(&_stopped) || !_vtSession) {
        return;
    }
    CVImageBufferRef imageBuffer = CMSampleBufferGetImageBuffer(sampleBuffer);
    if (!imageBuffer) {
        return;
    }
    CMTime pts = CMSampleBufferGetPresentationTimeStamp(sampleBuffer);
    if (!CMTIME_IS_VALID(pts)) {
        pts = CMTimeMake(0, _fps > 0 ? _fps : 60);
    }
    CMTime dur = CMTimeMake(1, _fps > 0 ? _fps : 60);

    NSDictionary *frameProps = nil;
    if (atomic_exchange(&_forceKey, false)) {
        frameProps = @{(__bridge NSString *)kVTEncodeFrameOptionKey_ForceKeyFrame: @YES};
    }

    VTCompressionSessionEncodeFrame(
        _vtSession,
        imageBuffer,
        pts,
        dur,
        (__bridge CFDictionaryRef)frameProps,
        NULL,
        NULL
    );
}

- (void)stream:(SCStream *)stream didStopWithError:(NSError *)error {
    (void)stream;
    (void)error;
    atomic_store(&_stopped, true);
}

@end

static void bh_apply_vt_bitrate(VTCompressionSessionRef session, int bitrateKbps) {
    if (!session || bitrateKbps <= 0) return;
    int64_t avgBps = (int64_t)bitrateKbps * 1000;
    CFNumberRef avgNum = CFNumberCreate(kCFAllocatorDefault, kCFNumberSInt64Type, &avgBps);
    if (avgNum) {
        VTSessionSetProperty(session, kVTCompressionPropertyKey_AverageBitRate, avgNum);
        CFRelease(avgNum);
    }
    int64_t peakBytesPerSec = (avgBps * 3) / 16; // 1.5x average bitrate in bytes/sec
    double oneSec = 1.0;
    CFNumberRef limitBytes = CFNumberCreate(kCFAllocatorDefault, kCFNumberSInt64Type, &peakBytesPerSec);
    CFNumberRef limitSec = CFNumberCreate(kCFAllocatorDefault, kCFNumberDoubleType, &oneSec);
    if (limitBytes && limitSec) {
        const void *vals[2] = {limitBytes, limitSec};
        CFArrayRef limits = CFArrayCreate(kCFAllocatorDefault, vals, 2, &kCFTypeArrayCallBacks);
        if (limits) {
            VTSessionSetProperty(session, kVTCompressionPropertyKey_DataRateLimits, limits);
            CFRelease(limits);
        }
    }
    if (limitBytes) CFRelease(limitBytes);
    if (limitSec) CFRelease(limitSec);
}

void *bh_sck_start(uintptr_t handle, int width, int height, int fps, int bitrateKbps, int isHEVC, char **errOut) {
    if (@available(macOS 12.3, *)) {
        __block SCShareableContent *content = nil;
        __block NSError *scErr = nil;
        dispatch_semaphore_t sem = dispatch_semaphore_create(0);
        [SCShareableContent getShareableContentExcludingDesktopWindows:NO
                                                  onScreenWindowsOnly:YES
                                                    completionHandler:^(SCShareableContent *shareableContent, NSError *error) {
            content = shareableContent;
            scErr = error;
            dispatch_semaphore_signal(sem);
        }];
        dispatch_time_t waitRes = dispatch_semaphore_wait(sem, dispatch_time(DISPATCH_TIME_NOW, 5 * NSEC_PER_SEC));
        if (waitRes != 0) {
            bh_set_err(errOut, @"获取 ScreenCaptureKit 显示列表超时");
            return NULL;
        }
        if (scErr || !content || content.displays.count == 0) {
            bh_set_err(errOut, scErr ? scErr.localizedDescription : @"未找到可采集的显示器");
            return NULL;
        }

        CGDirectDisplayID mainID = CGMainDisplayID();
        SCDisplay *targetDisplay = content.displays.firstObject;
        for (SCDisplay *d in content.displays) {
            if (d.displayID == mainID) {
                targetDisplay = d;
                break;
            }
        }

        BHScreenCapturer *cap = [[BHScreenCapturer alloc] init];
        cap.goHandle = handle;
        cap.fps = fps > 0 ? fps : 60;
        cap.isHEVC = isHEVC ? 1 : 0;
        atomic_init(&cap->_forceKey, true);
        atomic_init(&cap->_stopped, false);
        cap.sampleQueue = dispatch_queue_create("bothub.sck.sample", DISPATCH_QUEUE_SERIAL);

        CMVideoCodecType codecType = isHEVC ? kCMVideoCodecType_HEVC : kCMVideoCodecType_H264;
        VTCompressionSessionRef vt = NULL;
        OSStatus vtStatus = VTCompressionSessionCreate(
            kCFAllocatorDefault,
            width,
            height,
            codecType,
            NULL,
            NULL,
            NULL,
            bh_vt_output_callback,
            (__bridge void *)cap,
            &vt
        );
        if (vtStatus != noErr || !vt) {
            bh_set_err(errOut, [NSString stringWithFormat:@"VTCompressionSessionCreate failed: %d", (int)vtStatus]);
            return NULL;
        }

        VTSessionSetProperty(vt, kVTCompressionPropertyKey_RealTime, kCFBooleanTrue);
        VTSessionSetProperty(vt, kVTCompressionPropertyKey_AllowFrameReordering, kCFBooleanFalse);
        if (isHEVC) {
            VTSessionSetProperty(vt, kVTCompressionPropertyKey_ProfileLevel, kVTProfileLevel_HEVC_Main_AutoLevel);
        } else {
            VTSessionSetProperty(vt, kVTCompressionPropertyKey_ProfileLevel, kVTProfileLevel_H264_High_AutoLevel);
        }
        int gopFrames = cap.fps * 2;
        CFNumberRef gopNum = CFNumberCreate(kCFAllocatorDefault, kCFNumberIntType, &gopFrames);
        if (gopNum) {
            VTSessionSetProperty(vt, kVTCompressionPropertyKey_MaxKeyFrameInterval, gopNum);
            CFRelease(gopNum);
        }
        bh_apply_vt_bitrate(vt, bitrateKbps);
        VTCompressionSessionPrepareToEncodeFrames(vt);
        cap.vtSession = vt;

        SCContentFilter *filter = [[SCContentFilter alloc] initWithDisplay:targetDisplay excludingWindows:@[]];
        SCStreamConfiguration *cfg = [[SCStreamConfiguration alloc] init];
        cfg.width = (size_t)width;
        cfg.height = (size_t)height;
        cfg.pixelFormat = kCVPixelFormatType_420YpCbCr8BiPlanarVideoRange;
        cfg.minimumFrameInterval = CMTimeMake(1, cap.fps);
        cfg.showsCursor = YES;
        cfg.queueDepth = 3;
        if (@available(macOS 13.0, *)) {
            cfg.capturesAudio = NO;
        }

        NSError *addErr = nil;
        SCStream *stream = [[SCStream alloc] initWithFilter:filter configuration:cfg delegate:cap];
        if (![stream addStreamOutput:cap type:SCStreamOutputTypeScreen sampleHandlerQueue:cap.sampleQueue error:&addErr]) {
            VTCompressionSessionInvalidate(vt);
            CFRelease(vt);
            cap.vtSession = NULL;
            bh_set_err(errOut, addErr ? addErr.localizedDescription : @"SCStream addStreamOutput failed");
            return NULL;
        }
        cap.stream = stream;

        __block NSError *startErr = nil;
        dispatch_semaphore_t startSem = dispatch_semaphore_create(0);
        [stream startCaptureWithCompletionHandler:^(NSError *error) {
            startErr = error;
            dispatch_semaphore_signal(startSem);
        }];
        dispatch_time_t startWait = dispatch_semaphore_wait(startSem, dispatch_time(DISPATCH_TIME_NOW, 5 * NSEC_PER_SEC));
        if (startWait != 0 || startErr != nil) {
            [stream stopCaptureWithCompletionHandler:^(NSError * _Nullable error) { (void)error; }];
            VTCompressionSessionInvalidate(vt);
            CFRelease(vt);
            cap.vtSession = NULL;
            bh_set_err(errOut, startErr ? startErr.localizedDescription : @"SCStream startCapture timeout");
            return NULL;
        }

        return (void *)CFBridgingRetain(cap);
    } else {
        bh_set_err(errOut, @"需要 macOS 12.3+ 才能使用 ScreenCaptureKit");
        return NULL;
    }
}

void bh_sck_set_bitrate(void *ctx, int bitrateKbps) {
    if (!ctx) return;
    BHScreenCapturer *cap = (__bridge BHScreenCapturer *)ctx;
    if (atomic_load(&cap->_stopped) || !cap.vtSession) return;
    bh_apply_vt_bitrate(cap.vtSession, bitrateKbps);
}

void bh_sck_force_keyframe(void *ctx) {
    if (!ctx) return;
    BHScreenCapturer *cap = (__bridge BHScreenCapturer *)ctx;
    atomic_store(&cap->_forceKey, true);
}

void bh_sck_stop(void *ctx) {
    if (!ctx) return;
    BHScreenCapturer *cap = (BHScreenCapturer *)CFBridgingRelease(ctx);
    atomic_store(&cap->_stopped, true);
    if (cap.stream) {
        dispatch_semaphore_t sem = dispatch_semaphore_create(0);
        [cap.stream stopCaptureWithCompletionHandler:^(NSError * _Nullable error) {
            (void)error;
            dispatch_semaphore_signal(sem);
        }];
        dispatch_semaphore_wait(sem, dispatch_time(DISPATCH_TIME_NOW, 2 * NSEC_PER_SEC));
        cap.stream = nil;
    }
    if (cap.vtSession) {
        VTCompressionSessionCompleteFrames(cap.vtSession, kCMTimeInvalid);
        VTCompressionSessionInvalidate(cap.vtSession);
        CFRelease(cap.vtSession);
        cap.vtSession = NULL;
    }
}
