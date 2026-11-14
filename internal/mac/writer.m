#import "writer.h"
#import <UniformTypeIdentifiers/UniformTypeIdentifiers.h>
#import "_cgo_export.h"

@implementation OAWriter {
    AVAssetWriter *_writer;
    AVAssetWriterInput *_picture;
    AVAssetWriterInputPixelBufferAdaptor *_adaptor;
    NSMutableArray<AVAssetWriterInput *> *_sound;
    CMTime _start;
    CMTime _lastPicture;
    BOOL _realTime;
}

- (instancetype)initWithWidth:(int)width height:(int)height fps:(int)fps pieceSeconds:(double)seconds bitrate:(int)bitrate realTime:(BOOL)realTime {
    if (!(self = [super init])) return nil;
    _realTime = realTime;
    _sound = [NSMutableArray array];
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
        return (int)_sound.count - 1;
    } @catch (NSException *e) {
        return -1;
    }
}

- (BOOL)startAt:(CMTime)time error:(NSError **)error {
    @try {
        _writer.initialSegmentStartTime = time;
        if (![_writer startWriting]) {
            if (error) *error = _writer.error;
            return NO;
        }
        [_writer startSessionAtSourceTime:time];
    } @catch (NSException *e) {
        if (error) *error = [NSError errorWithDomain:@"onair" code:1 userInfo:@{NSLocalizedDescriptionKey : e.reason ?: e.name}];
        return NO;
    }
    _start = time;
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

- (BOOL)appendPicture:(CVPixelBufferRef)pixels at:(CMTime)time {
    if (!_started || _writer.status != AVAssetWriterStatusWriting) return NO;
    // Time only moves forward in a track.
    if (CMTIME_IS_VALID(_lastPicture) && CMTimeCompare(time, _lastPicture) <= 0) return NO;
    if (CMTimeCompare(time, _start) < 0) return NO;
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
    if (CMTimeCompare(CMSampleBufferGetPresentationTimeStamp(samples), _start) < 0) return NO;
    AVAssetWriterInput *input = _sound[track];
    if (![self ready:input]) {
        _droppedSound++;
        return NO;
    }
    @try {
        return [input appendSampleBuffer:samples];
    } @catch (NSException *e) {
        return NO;
    }
}

- (void)finish:(void (^)(NSError *error))done {
    if (!_started || _writer.status != AVAssetWriterStatusWriting) {
        done(_writer.error);
        return;
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
