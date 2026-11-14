#import "writer.h"
#import <UniformTypeIdentifiers/UniformTypeIdentifiers.h>
#import "_cgo_export.h"

// Sound the writer isn't ready for waits, up to this many buffers a track:
// four seconds of the mixer's. A buffer left out isn't a gap in the track.
// The track closes up, and all the sound after it plays early.
enum { kWaiting = 47 };

@implementation OAWriter {
    AVAssetWriter *_writer;
    AVAssetWriterInput *_picture;
    AVAssetWriterInputPixelBufferAdaptor *_adaptor;
    NSMutableArray<AVAssetWriterInput *> *_sound;
    NSMutableArray<NSMutableArray *> *_waiting; // sound not yet taken, a list a track
    CMTime _origin;      // the first frame's time on capture's clock
    CMTime _lastPicture; // on the recording's timeline
    BOOL _realTime;
}

- (instancetype)initWithWidth:(int)width height:(int)height fps:(int)fps pieceSeconds:(double)seconds bitrate:(int)bitrate realTime:(BOOL)realTime {
    if (!(self = [super init])) return nil;
    _realTime = realTime;
    _sound = [NSMutableArray array];
    _waiting = [NSMutableArray array];
    _lastPicture = kCMTimeInvalid;
    _writer = [[AVAssetWriter alloc] initWithContentType:UTTypeMPEG4Movie];
    // Segments, not a file: each is handed to the delegate when it is whole,
    // so nothing waits for the end of the recording.
    _writer.outputFileTypeProfile = AVFileTypeProfileMPEG4AppleHLS;
    _writer.preferredOutputSegmentInterval = CMTimeMakeWithSeconds(seconds, 600);
    _writer.delegate = self;
    NSDictionary *settings = @{
        AVVideoCodecKey : AVVideoCodecTypeH264,
        AVVideoWidthKey : @(width),
        AVVideoHeightKey : @(height),
        AVVideoCompressionPropertiesKey : @{
            AVVideoAverageBitRateKey : @(bitrate),
            // A segment starts on a key frame, so one is due every piece.
            AVVideoMaxKeyFrameIntervalDurationKey : @(seconds),
            AVVideoExpectedSourceFrameRateKey : @(fps),
            AVVideoAllowFrameReorderingKey : @NO,
            AVVideoProfileLevelKey : AVVideoProfileLevelH264HighAutoLevel,
        },
    };
    _picture = [AVAssetWriterInput assetWriterInputWithMediaType:AVMediaTypeVideo outputSettings:settings];
    _picture.expectsMediaDataInRealTime = realTime;
    _adaptor = [AVAssetWriterInputPixelBufferAdaptor assetWriterInputPixelBufferAdaptorWithAssetWriterInput:_picture sourcePixelBufferAttributes:nil];
    [_writer addInput:_picture];
    return self;
}

- (int)addSoundTrack:(CMFormatDescriptionRef)format {
    const AudioStreamBasicDescription *asbd = format ? CMAudioFormatDescriptionGetStreamBasicDescription(format) : NULL;
    double rate = asbd && asbd->mSampleRate > 0 ? asbd->mSampleRate : 48000;
    int channels = asbd && asbd->mChannelsPerFrame > 0 ? (int)asbd->mChannelsPerFrame : 2;
    if (channels > 2) channels = 2;
    NSDictionary *settings = @{
        AVFormatIDKey : @(kAudioFormatMPEG4AAC),
        AVSampleRateKey : @(rate),
        AVNumberOfChannelsKey : @(channels),
        AVEncoderBitRateKey : @(channels == 1 ? 64000 : 128000),
    };
    @try {
        AVAssetWriterInput *input = [AVAssetWriterInput assetWriterInputWithMediaType:AVMediaTypeAudio outputSettings:settings sourceFormatHint:format];
        input.expectsMediaDataInRealTime = _realTime;
        if (![_writer canAddInput:input]) return -1;
        [_writer addInput:input];
        [_sound addObject:input];
        [_waiting addObject:[NSMutableArray array]];
        return (int)_sound.count - 1;
    } @catch (NSException *e) {
        return -1;
    }
}

- (BOOL)startAt:(CMTime)origin error:(NSError **)error {
    if (!CMTIME_IS_NUMERIC(origin)) {
        if (error) *error = [NSError errorWithDomain:@"onair" code:1 userInfo:@{NSLocalizedDescriptionKey : @"the first frame has no time"}];
        return NO;
    }
    @try {
        // Capture's clock counts from when the machine started. A recording
        // counts from its first frame, in the joined file and in every
        // piece, or whatever reads its times sees it start days in.
        _writer.initialSegmentStartTime = kCMTimeZero;
        if (![_writer startWriting]) {
            if (error) *error = _writer.error;
            return NO;
        }
        [_writer startSessionAtSourceTime:kCMTimeZero];
    } @catch (NSException *e) {
        if (error) *error = [NSError errorWithDomain:@"onair" code:1 userInfo:@{NSLocalizedDescriptionKey : e.reason ?: e.name}];
        return NO;
    }
    _origin = origin;
    _started = YES;
    return YES;
}

// A real-time input that isn't ready drops what it is offered. A made-up
// source can wait for it.
- (BOOL)ready:(AVAssetWriterInput *)input {
    if (_realTime) return input.isReadyForMoreMediaData;
    for (int i = 0; i < 5000 && !input.isReadyForMoreMediaData && _writer.status == AVAssetWriterStatusWriting; i++) usleep(1000);
    return input.isReadyForMoreMediaData;
}

- (BOOL)appendPicture:(CVPixelBufferRef)pixels at:(CMTime)taken {
    if (!_started || _writer.status != AVAssetWriterStatusWriting) return NO;
    CMTime time = CMTimeSubtract(taken, _origin);
    // Time only moves forward in a track.
    if (!CMTIME_IS_NUMERIC(time) || CMTimeCompare(time, kCMTimeZero) < 0) return NO;
    if (CMTIME_IS_VALID(_lastPicture) && CMTimeCompare(time, _lastPicture) <= 0) return NO;
    for (int track = 0; track < (int)_sound.count; track++) [self catchUp:track];
    if (![self ready:_picture]) {
        _droppedFrames++;
        return NO;
    }
    @try {
        if (![_adaptor appendPixelBuffer:pixels withPresentationTime:time]) return NO;
    } @catch (NSException *e) {
        return NO;
    }
    _lastPicture = time;
    return YES;
}

- (BOOL)appendSound:(CMSampleBufferRef)samples track:(int)track {
    if (!_started || track < 0 || track >= (int)_sound.count || _writer.status != AVAssetWriterStatusWriting) return NO;
    if (CMTimeCompare(CMSampleBufferGetPresentationTimeStamp(samples), kCMTimeZero) < 0) return NO;
    [_waiting[track] addObject:(__bridge id)samples];
    return [self catchUp:track];
}

// CatchUp gives a track's input the sound waiting for it, in order, as far
// as it will take it. Out of real time, as in the self-test, it waits for
// the input. In real time it doesn't: what is left goes in with a later
// buffer or picture, and only sound that has waited seconds is dropped.
- (BOOL)catchUp:(int)track {
    NSMutableArray *waiting = _waiting[track];
    AVAssetWriterInput *input = _sound[track];
    while (waiting.count) {
        if (![self ready:input]) {
            if (!_realTime) return NO;
            break;
        }
        BOOL ok = NO;
        @try {
            ok = [input appendSampleBuffer:(__bridge CMSampleBufferRef)waiting.firstObject];
        } @catch (NSException *e) {
        }
        [waiting removeObjectAtIndex:0];
        if (!ok) return NO;
    }
    while (waiting.count > kWaiting) {
        [waiting removeObjectAtIndex:0];
        _droppedSound++;
    }
    return YES;
}

- (void)finish:(void (^)(NSError *error))done {
    if (!_started || _writer.status != AVAssetWriterStatusWriting) {
        done(_writer.error);
        return;
    }
    // What still waits goes in now, given a moment at most, so Stop stays
    // quick.
    for (int track = 0; track < (int)_sound.count; track++) {
        for (int i = 0; i < 1000 && [_waiting[track] count] && _writer.status == AVAssetWriterStatusWriting; i++) {
            if (![self catchUp:track] && !_realTime) break;
            if ([_waiting[track] count]) usleep(1000);
        }
        _droppedSound += (long)[_waiting[track] count];
        [_waiting[track] removeAllObjects];
    }
    [_picture markAsFinished];
    for (AVAssetWriterInput *input in _sound) [input markAsFinished];
    AVAssetWriter *writer = _writer;
    [writer finishWritingWithCompletionHandler:^{
        done(writer.status == AVAssetWriterStatusCompleted ? nil : writer.error);
    }];
}

- (NSError *)error {
    return _writer.error;
}

- (void)assetWriter:(AVAssetWriter *)writer didOutputSegmentData:(NSData *)data segmentType:(AVAssetSegmentType)type segmentReport:(AVAssetSegmentReport *)report {
    onairPiece(type == AVAssetSegmentTypeInitialization ? 0 : 1, (void *)data.bytes, (long)data.length);
}

@end
