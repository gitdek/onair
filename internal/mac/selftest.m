#import "recorder.h"
#import "writer.h"
#import "mixer.h"
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

// A float format for made-up sound: interleaved, or one plane a channel as
// capture's display sound comes.
static CMAudioFormatDescriptionRef madeUpFormat(double rate, int channels, BOOL planar) {
    UInt32 bytes = planar ? 4 : 4 * channels;
    AudioStreamBasicDescription asbd = {
        .mSampleRate = rate,
        .mFormatID = kAudioFormatLinearPCM,
        .mFormatFlags = kAudioFormatFlagIsFloat | kAudioFormatFlagIsPacked | (planar ? kAudioFormatFlagIsNonInterleaved : 0),
        .mBytesPerPacket = bytes,
        .mFramesPerPacket = 1,
        .mBytesPerFrame = bytes,
        .mChannelsPerFrame = channels,
        .mBitsPerChannel = 32,
    };
    CMAudioFormatDescriptionRef format = NULL;
    CMAudioFormatDescriptionCreate(kCFAllocatorDefault, &asbd, 0, NULL, 0, NULL, NULL, &format);
    return format;
}

// A buffer of a tone in format, each channel at its own loudness. The phase
// carries on from one buffer to the next.
static CMSampleBufferRef madeUpSound(CMAudioFormatDescriptionRef format, CMTime time, int frames, double hertz, const float *loudness, double *phase) {
    const AudioStreamBasicDescription *asbd = CMAudioFormatDescriptionGetStreamBasicDescription(format);
    int channels = (int)asbd->mChannelsPerFrame;
    BOOL planar = (asbd->mFormatFlags & kAudioFormatFlagIsNonInterleaved) != 0;
    size_t bytes = (size_t)frames * channels * sizeof(float);
    CMBlockBufferRef block = NULL;
    if (CMBlockBufferCreateWithMemoryBlock(kCFAllocatorDefault, NULL, bytes, kCFAllocatorDefault, NULL, 0, bytes, kCMBlockBufferAssureMemoryNowFlag, &block) != noErr) return NULL;
    char *data = NULL;
    CMBlockBufferGetDataPointer(block, 0, NULL, NULL, &data);
    float *out = (float *)data;
    for (int i = 0; i < frames; i++) {
        double v = sin(*phase);
        *phase = fmod(*phase + 2 * M_PI * hertz / asbd->mSampleRate, 2 * M_PI);
        for (int c = 0; c < channels; c++) out[planar ? c * frames + i : i * channels + c] = (float)(loudness[c] * v);
    }
    CMSampleBufferRef samples = NULL;
    CMAudioSampleBufferCreateReadyWithPacketDescriptions(kCFAllocatorDefault, block, format, frames, time, NULL, &samples);
    CFRelease(block);
    return samples;
}

static NSString *finished(OAWriter *writer) {
    dispatch_semaphore_t done = dispatch_semaphore_create(0);
    __block NSError *ended = nil;
    [writer finish:^(NSError *e) {
        ended = e;
        dispatch_semaphore_signal(done);
    }];
    dispatch_semaphore_wait(done, DISPATCH_TIME_FOREVER);
    return ended ? ended.localizedDescription ?: @"the writer failed" : nil;
}

static NSString *started(OAWriter *writer, CMTime start) {
    NSError *failed = nil;
    if ([writer startAt:start error:&failed]) return nil;
    return [NSString stringWithFormat:@"the writer didn't start: %@ %@ %@", failed.localizedDescription ?: @"", failed.localizedFailureReason ?: @"", failed.userInfo[NSUnderlyingErrorKey] ?: @""];
}

int onair_selftest(double seconds, int audioTracks, double pieceSeconds, char **error) {
    @autoreleasepool {
        const int width = 1280, height = 720, fps = 15;
        OAWriter *writer = [[OAWriter alloc] initWithWidth:width height:height fps:fps pieceSeconds:pieceSeconds bitrate:1500000 realTime:NO];
        CMAudioFormatDescriptionRef format = madeUpFormat(48000, 2, NO);
        const float loudness[2] = {0.2f, 0.2f};
        for (int i = 0; i < audioTracks; i++) {
            if ([writer addSoundTrack:format] != i) {
                *error = message([NSString stringWithFormat:@"the writer refused sound track %d", i + 1]);
                CFRelease(format);
                return -1;
            }
        }
        // Capture's timestamps are the host clock's, so start from it too.
        CMTime start = CMClockGetTime(CMClockGetHostTimeClock());
        NSString *failed = started(writer, start);
        double phases[4] = {0};
        int frames = (int)(seconds * fps);
        for (int i = 0; i < frames && !failed; i++) {
            CMTime time = CMTimeAdd(start, CMTimeMake(i, fps));
            CVPixelBufferRef pixels = madeUpFrame(width, height, i);
            BOOL ok = pixels && [writer appendPicture:pixels at:time];
            if (pixels) CVPixelBufferRelease(pixels);
            if (!ok) {
                failed = [NSString stringWithFormat:@"frame %d wasn't written: %@", i, writer.error.localizedDescription ?: @"no reason given"];
                break;
            }
            // Sound goes to the writer on the recording's timeline, from zero,
            // as the mixer hands it on.
            for (int t = 0; t < audioTracks && !failed; t++) {
                CMSampleBufferRef samples = madeUpSound(format, CMTimeMake(i * (48000 / fps), 48000), 48000 / fps, 440 * (t + 1), loudness, &phases[t]);
                ok = samples && [writer appendSound:samples track:t];
                if (samples) CFRelease(samples);
                if (!ok) failed = [NSString stringWithFormat:@"sound for frame %d, track %d, wasn't written: %@", i, t + 1, writer.error.localizedDescription ?: @"no reason given"];
            }
        }
        if (!failed) failed = finished(writer);
        CFRelease(format);
        if (failed) {
            *error = message(failed);
            return -1;
        }
        return 0;
    }
}

// What reaches capture's queue, in the order it arrives.
typedef struct {
    double arrives; // seconds from the start
    double at;      // its stamp
    int kind;       // 0 a frame, 1 display sound, 2 the microphone, 3 the timer
    int frames;     // sound frames
    int rate;       // the microphone's rate
} event;

static int byArrival(const void *a, const void *b) {
    double x = ((const event *)a)->arrives, y = ((const event *)b)->arrives;
    return x < y ? -1 : x > y;
}

// A stamp's wobble, up to half a millisecond either way, the same every run.
static double wobble(unsigned *seed) {
    *seed = *seed * 1103515245u + 12345u;
    return ((double)((*seed >> 8) % 1001) / 1000.0 - 0.5) / 1000.0;
}

int onair_selftest_mix(onair_mix plan, onair_mix_report *report, char **error) {
    @autoreleasepool {
        const int width = 1280, height = 720, fps = 15;
        OAWriter *writer = [[OAWriter alloc] initWithWidth:width height:height fps:fps pieceSeconds:plan.pieceSeconds bitrate:1500000 realTime:NO];
        __block long refused = 0;
        OAMixer *mixer = [[OAMixer alloc] initWithSources:2 sink:^(CMSampleBufferRef samples) {
            if (![writer appendSound:samples track:0]) refused++;
        }];
        if (!mixer || [writer addSoundTrack:mixer.format] != 0) {
            *error = message(@"the writer refused the mixer's sound track");
            return -1;
        }
        // Shaped like capture's: the display's sound in stereo planes at
        // 48 kHz, 1024 frames a buffer; the microphone in mono at 44.1 kHz,
        // 441 frames a buffer, stamped a few milliseconds off the display's,
        // then at 48 kHz after it switches.
        CMAudioFormatDescriptionRef display = madeUpFormat(48000, 2, YES);
        CMAudioFormatDescriptionRef mic44 = madeUpFormat(44100, 1, NO);
        CMAudioFormatDescriptionRef mic48 = madeUpFormat(48000, 1, NO);
        const float displayLoudness[2] = {0.2f, 0.1f}, micLoudness[1] = {0.15f};
        NSMutableData *events = [NSMutableData data];
        unsigned seed = 2;
        for (int i = 0; i < (int)(plan.seconds * fps); i++) {
            event e = {.at = (double)i / fps, .arrives = (double)i / fps + 0.005, .kind = 0};
            [events appendBytes:&e length:sizeof e];
        }
        for (long k = 0;; k++) {
            double at = k * 1024 / 48000.0;
            if (at >= plan.seconds) break;
            event e = {.at = at + wobble(&seed), .arrives = at + 1024 / 48000.0 + 0.010, .kind = 1, .frames = 1024};
            [events appendBytes:&e length:sizeof e];
        }
        for (double at = 0.0037; at < plan.seconds;) {
            int rate = at < plan.switchAt ? 44100 : 48000, frames = rate / 100;
            if (at < plan.quietFrom || at >= plan.quietTo) {
                event e = {.at = at + wobble(&seed), .arrives = at + 0.010 + 0.015, .kind = 2, .frames = frames, .rate = rate};
                [events appendBytes:&e length:sizeof e];
            }
            at += (double)frames / rate;
        }
        for (double at = 0.5; at < plan.seconds; at += 0.5) {
            event e = {.at = at, .arrives = at, .kind = 3};
            [events appendBytes:&e length:sizeof e];
        }
        event *list = events.mutableBytes;
        size_t count = events.length / sizeof(event);
        qsort(list, count, sizeof(event), byArrival);

        CMTime start = CMClockGetTime(CMClockGetHostTimeClock());
        NSString *failed = started(writer, start);
        if (!failed) [mixer startAt:start];
        double displayPhase = 0, micPhase = 0;
        int frame = 0;
        for (size_t i = 0; i < count && !failed; i++) {
            event e = list[i];
            CMTime at = CMTimeAdd(start, CMTimeMakeWithSeconds(e.at, 1000000000));
            if (e.kind == 0) {
                CVPixelBufferRef pixels = madeUpFrame(width, height, frame);
                BOOL ok = pixels && [writer appendPicture:pixels at:CMTimeAdd(start, CMTimeMake(frame, fps))];
                if (pixels) CVPixelBufferRelease(pixels);
                if (!ok) failed = [NSString stringWithFormat:@"frame %d wasn't written: %@", frame, writer.error.localizedDescription ?: @"no reason given"];
                frame++;
            } else if (e.kind == 3) {
                [mixer advanceTo:at];
            } else {
                BOOL mic = e.kind == 2;
                CMSampleBufferRef samples = mic ? madeUpSound(e.rate == 44100 ? mic44 : mic48, at, e.frames, 880, micLoudness, &micPhase)
                                                : madeUpSound(display, at, e.frames, 440, displayLoudness, &displayPhase);
                if (!samples) {
                    failed = @"made-up sound couldn't be made";
                    break;
                }
                [mixer add:samples source:mic ? 1 : 0];
                CFRelease(samples);
            }
            if (!failed && refused) failed = [NSString stringWithFormat:@"the writer refused mixed sound: %@", writer.error.localizedDescription ?: @"no reason given"];
        }
        if (!failed) {
            [mixer flush];
            if (refused) failed = [NSString stringWithFormat:@"the writer refused mixed sound: %@", writer.error.localizedDescription ?: @"no reason given"];
        }
        if (!failed) failed = finished(writer);
        CFRelease(display);
        CFRelease(mic44);
        CFRelease(mic48);
        report->late = mixer.late;
        report->early = mixer.early;
        report->handed = mixer.handed;
        report->dropped = refused;
        if (failed) {
            *error = message(failed);
            return -1;
        }
        return 0;
    }
}

int onair_probe(const char *path, onair_movie *movie, char **error) {
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
        NSArray<AVAssetTrack *> *pictures = [asset tracksWithMediaType:AVMediaTypeVideo], *sounds = [asset tracksWithMediaType:AVMediaTypeAudio];
        movie->seconds = CMTimeGetSeconds(asset.duration);
        movie->videoTracks = (int)pictures.count;
        movie->audioTracks = (int)sounds.count;
        movie->videoStart = pictures.count ? CMTimeGetSeconds(pictures[0].timeRange.start) : 0;
        movie->audioStart = sounds.count ? CMTimeGetSeconds(sounds[0].timeRange.start) : 0;
        return 0;
    }
}

// The first sample's time in a movie's track, read as a player would.
static BOOL firstTime(AVAsset *asset, AVAssetTrack *track, CMTime *time, NSError **error) {
    AVAssetReader *reader = [AVAssetReader assetReaderWithAsset:asset error:error];
    if (!reader) return NO;
    AVAssetReaderTrackOutput *output = [AVAssetReaderTrackOutput assetReaderTrackOutputWithTrack:track outputSettings:nil];
    [reader addOutput:output];
    if (![reader startReading]) {
        if (error) *error = reader.error;
        return NO;
    }
    CMSampleBufferRef samples = [output copyNextSampleBuffer];
    if (!samples) {
        if (error) *error = reader.error;
        return NO;
    }
    *time = CMSampleBufferGetPresentationTimeStamp(samples);
    CFRelease(samples);
    [reader cancelReading];
    return YES;
}

int onair_sound(const char *path, float **samples, long *frames, double *start, char **error) {
    @autoreleasepool {
        NSURL *url = [NSURL fileURLWithPath:[NSString stringWithUTF8String:path]];
        AVURLAsset *asset = [AVURLAsset URLAssetWithURL:url options:@{AVURLAssetPreferPreciseDurationAndTimingKey : @YES}];
        NSArray<AVAssetTrack *> *pictures = [asset tracksWithMediaType:AVMediaTypeVideo], *sounds = [asset tracksWithMediaType:AVMediaTypeAudio];
        if (pictures.count != 1 || sounds.count != 1) {
            *error = message([NSString stringWithFormat:@"it has %lu picture and %lu sound tracks, not one of each", (unsigned long)pictures.count, (unsigned long)sounds.count]);
            return -1;
        }
        NSError *failed = nil;
        CMTime picture;
        if (!firstTime(asset, pictures[0], &picture, &failed)) {
            *error = message([NSString stringWithFormat:@"its first frame can't be read: %@", failed.localizedDescription ?: @"no reason given"]);
            return -1;
        }
        AVAssetReader *reader = [AVAssetReader assetReaderWithAsset:asset error:&failed];
        AVAssetReaderTrackOutput *output = [AVAssetReaderTrackOutput assetReaderTrackOutputWithTrack:sounds[0] outputSettings:@{
            AVFormatIDKey : @(kAudioFormatLinearPCM),
            AVLinearPCMIsFloatKey : @YES,
            AVLinearPCMBitDepthKey : @32,
            AVLinearPCMIsNonInterleaved : @NO,
            AVLinearPCMIsBigEndianKey : @NO,
            AVSampleRateKey : @48000,
            AVNumberOfChannelsKey : @2,
        }];
        if (!reader || ![reader canAddOutput:output]) {
            *error = message([NSString stringWithFormat:@"its sound can't be read: %@", failed.localizedDescription ?: @"no reason given"]);
            return -1;
        }
        [reader addOutput:output];
        if (![reader startReading]) {
            *error = message([NSString stringWithFormat:@"its sound can't be read: %@", reader.error.localizedDescription ?: @"no reason given"]);
            return -1;
        }
        NSMutableData *pcm = [NSMutableData data];
        CMTime first = kCMTimeInvalid;
        for (CMSampleBufferRef buffer; (buffer = [output copyNextSampleBuffer]);) {
            if (CMTIME_IS_INVALID(first)) first = CMSampleBufferGetPresentationTimeStamp(buffer);
            AudioBufferList list;
            CMBlockBufferRef block = NULL;
            if (CMSampleBufferGetAudioBufferListWithRetainedBlockBuffer(buffer, NULL, &list, sizeof list, NULL, NULL, 0, &block) == noErr) {
                [pcm appendBytes:list.mBuffers[0].mData length:list.mBuffers[0].mDataByteSize];
                CFRelease(block);
            }
            CFRelease(buffer);
        }
        if (reader.status != AVAssetReaderStatusCompleted || CMTIME_IS_INVALID(first)) {
            *error = message([NSString stringWithFormat:@"its sound wasn't read to the end: %@", reader.error.localizedDescription ?: @"no sound came"]);
            return -1;
        }
        *samples = malloc(pcm.length);
        memcpy(*samples, pcm.bytes, pcm.length);
        *frames = (long)(pcm.length / (2 * sizeof(float)));
        *start = CMTimeGetSeconds(CMTimeSubtract(first, picture));
        return 0;
    }
}
