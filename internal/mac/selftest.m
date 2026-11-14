#import "recorder.h"
#import "writer.h"
#import <AVFoundation/AVFoundation.h>

static char *message(NSString *text) {
    return strdup(text.UTF8String ?: "unknown error");
}

// A frame with a bar that moves, so consecutive frames differ.
static CVPixelBufferRef madeUpFrame(int width, int height, int index) {
    CVPixelBufferRef pixels = NULL;
    NSDictionary *attrs = @{(id)kCVPixelBufferIOSurfacePropertiesKey : @{}};
    if (CVPixelBufferCreate(kCFAllocatorDefault, width, height, kCVPixelFormatType_32BGRA, (__bridge CFDictionaryRef)attrs, &pixels) != kCVReturnSuccess) return NULL;
    CVPixelBufferLockBaseAddress(pixels, 0);
    uint8_t *base = CVPixelBufferGetBaseAddress(pixels);
    size_t stride = CVPixelBufferGetBytesPerRow(pixels);
    int bar = (index * 16) % width;
    for (int y = 0; y < height; y++) {
        uint32_t *row = (uint32_t *)(base + y * stride);
        for (int x = 0; x < width; x++) row[x] = (x >= bar && x < bar + 64) ? 0xFFE0453A : 0xFF202428;
    }
    CVPixelBufferUnlockBaseAddress(pixels, 0);
    return pixels;
}

static CMSampleBufferRef madeUpSound(CMAudioFormatDescriptionRef format, CMTime time, int frames, double hertz, double *phase) {
    size_t bytes = (size_t)frames * 2 * sizeof(float);
    CMBlockBufferRef block = NULL;
    if (CMBlockBufferCreateWithMemoryBlock(kCFAllocatorDefault, NULL, bytes, kCFAllocatorDefault, NULL, 0, bytes, kCMBlockBufferAssureMemoryNowFlag, &block) != noErr) return NULL;
    char *data = NULL;
    CMBlockBufferGetDataPointer(block, 0, NULL, NULL, &data);
    float *out = (float *)data;
    for (int i = 0; i < frames; i++) {
        float v = 0.2f * sinf((float)*phase);
        *phase += 2 * M_PI * hertz / 48000;
        out[2 * i] = v;
        out[2 * i + 1] = v;
    }
    CMSampleBufferRef samples = NULL;
    CMAudioSampleBufferCreateReadyWithPacketDescriptions(kCFAllocatorDefault, block, format, frames, time, NULL, &samples);
    CFRelease(block);
    return samples;
}

int onair_selftest(double seconds, int audioTracks, double pieceSeconds, char **error) {
    @autoreleasepool {
        const int width = 1280, height = 720, fps = 15;
        OAWriter *writer = [[OAWriter alloc] initWithWidth:width height:height fps:fps pieceSeconds:pieceSeconds bitrate:1500000 realTime:NO];
        AudioStreamBasicDescription asbd = {
            .mSampleRate = 48000,
            .mFormatID = kAudioFormatLinearPCM,
            .mFormatFlags = kAudioFormatFlagIsFloat | kAudioFormatFlagIsPacked,
            .mBytesPerPacket = 8,
            .mFramesPerPacket = 1,
            .mBytesPerFrame = 8,
            .mChannelsPerFrame = 2,
            .mBitsPerChannel = 32,
        };
        CMAudioFormatDescriptionRef format = NULL;
        CMAudioFormatDescriptionCreate(kCFAllocatorDefault, &asbd, 0, NULL, 0, NULL, NULL, &format);
        for (int i = 0; i < audioTracks; i++) {
            if ([writer addSoundTrack:format] != i) {
                *error = message([NSString stringWithFormat:@"the writer refused sound track %d", i + 1]);
                return -1;
            }
        }
        // Capture's timestamps are the host clock's, so start from it too.
        CMTime start = CMClockGetTime(CMClockGetHostTimeClock());
        NSError *failed = nil;
        if (![writer startAt:start error:&failed]) {
            *error = message([NSString stringWithFormat:@"the writer didn't start: %@ %@ %@", failed.localizedDescription ?: @"", failed.localizedFailureReason ?: @"", failed.userInfo[NSUnderlyingErrorKey] ?: @""]);
            return -1;
        }
        double phases[4] = {0};
        int frames = (int)(seconds * fps);
        for (int i = 0; i < frames; i++) {
            CMTime time = CMTimeAdd(start, CMTimeMake(i, fps));
            CVPixelBufferRef pixels = madeUpFrame(width, height, i);
            BOOL ok = pixels && [writer appendPicture:pixels at:time];
            if (pixels) CVPixelBufferRelease(pixels);
            if (!ok) {
                *error = message([NSString stringWithFormat:@"frame %d wasn't written: %@", i, writer.error.localizedDescription ?: @"no reason given"]);
                return -1;
            }
            for (int t = 0; t < audioTracks; t++) {
                CMSampleBufferRef samples = madeUpSound(format, time, 48000 / fps, 440 * (t + 1), &phases[t]);
                ok = samples && [writer appendSound:samples track:t];
                if (samples) CFRelease(samples);
                if (!ok) {
                    *error = message([NSString stringWithFormat:@"sound for frame %d, track %d, wasn't written: %@", i, t + 1, writer.error.localizedDescription ?: @"no reason given"]);
                    return -1;
                }
            }
        }
        dispatch_semaphore_t done = dispatch_semaphore_create(0);
        __block NSError *ended = nil;
        [writer finish:^(NSError *e) {
            ended = e;
            dispatch_semaphore_signal(done);
        }];
        dispatch_semaphore_wait(done, DISPATCH_TIME_FOREVER);
        if (format) CFRelease(format);
        if (ended) {
            *error = message(ended.localizedDescription);
            return -1;
        }
        return 0;
    }
}

int onair_probe(const char *path, double *seconds, int *videoTracks, int *audioTracks, char **error) {
    @autoreleasepool {
        NSURL *url = [NSURL fileURLWithPath:[NSString stringWithUTF8String:path]];
        AVURLAsset *asset = [AVURLAsset URLAssetWithURL:url options:@{AVURLAssetPreferPreciseDurationAndTimingKey : @YES}];
        dispatch_semaphore_t loaded = dispatch_semaphore_create(0);
        [asset loadValuesAsynchronouslyForKeys:@[ @"duration", @"tracks", @"playable" ] completionHandler:^{
            dispatch_semaphore_signal(loaded);
        }];
        if (dispatch_semaphore_wait(loaded, dispatch_time(DISPATCH_TIME_NOW, 60 * NSEC_PER_SEC)) != 0) {
            *error = message(@"the file took more than a minute to read");
            return -1;
        }
        NSError *failed = nil;
        for (NSString *key in @[ @"duration", @"tracks", @"playable" ]) {
            if ([asset statusOfValueForKey:key error:&failed] != AVKeyValueStatusLoaded) {
                *error = message(failed.localizedDescription ?: [NSString stringWithFormat:@"its %@ can't be read", key]);
                return -1;
            }
        }
        if (!asset.playable) {
            *error = message(@"it isn't playable");
            return -1;
        }
        *seconds = CMTimeGetSeconds(asset.duration);
        *videoTracks = (int)[asset tracksWithMediaType:AVMediaTypeVideo].count;
        *audioTracks = (int)[asset tracksWithMediaType:AVMediaTypeAudio].count;
        return 0;
    }
}
