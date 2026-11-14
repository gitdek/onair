#import "recorder.h"
#import "writer.h"
#import <ScreenCaptureKit/ScreenCaptureKit.h>
#import <CoreGraphics/CoreGraphics.h>
#import "_cgo_export.h"

// OACapture takes the main display's picture and sound from ScreenCaptureKit
// and gives them to the writer. It keeps the picture moving when the screen
// is still: a segment ends on a key frame, so with no frames no piece would
// close, and a kill would cost all the sound since the last one.
@interface OACapture : NSObject <SCStreamOutput, SCStreamDelegate>
- (void)start:(onair_config)cfg;
- (void)stop;
@end

static void report(int code, NSString *text) {
    onairEvent(code, (char *)(text.UTF8String ?: ""));
}

@implementation OACapture {
    dispatch_queue_t _queue;
    dispatch_source_t _timer;
    SCStream *_stream;
    OAWriter *_writer;
    onair_config _cfg;
    int _width, _height;
    CVPixelBufferRef _last;        // the newest whole frame
    CMTime _lastWritten;           // when a frame was last written, on the stream's clock
    CMTime _clockOffset;           // the stream's clock minus the host's
    CMFormatDescriptionRef _soundFormat;
    CMTime _firstFrame;            // when the first whole frame came
    BOOL _stopping, _ended;
}

- (instancetype)init {
    if (!(self = [super init])) return nil;
    _queue = dispatch_queue_create("onair.capture", DISPATCH_QUEUE_SERIAL);
    _lastWritten = kCMTimeInvalid;
    _clockOffset = kCMTimeZero;
    _firstFrame = kCMTimeInvalid;
    return self;
}

- (void)start:(onair_config)cfg {
    _cfg = cfg;
    [SCShareableContent getShareableContentExcludingDesktopWindows:NO onScreenWindowsOnly:YES completionHandler:^(SCShareableContent *content, NSError *error) {
        dispatch_async(self->_queue, ^{
            if (error || content.displays.count == 0) {
                // Without the permission macOS answers with an error, having
                // shown its own request the first time.
                [self end:[NSString stringWithFormat:@"On Air can't see the screen: %@. Allow it in System Settings, Privacy & Security, Screen & System Audio Recording, then start again.", error.localizedDescription ?: @"no display was found"]];
                return;
            }
            SCDisplay *display = content.displays.firstObject;
            for (SCDisplay *d in content.displays) {
                if (d.displayID == CGMainDisplayID()) display = d;
            }
            [self begin:display];
        });
    }];
}

- (void)begin:(SCDisplay *)display {
    SCContentFilter *filter = [[SCContentFilter alloc] initWithDisplay:display excludingWindows:@[]];
    // The display's size in pixels, brought down to the rows asked for, with
    // even sides as the encoder wants.
    double scale = filter.pointPixelScale > 0 ? filter.pointPixelScale : 1;
    double width = display.width * scale, height = display.height * scale;
    if (_cfg.rows > 0 && height > _cfg.rows) {
        width = width * _cfg.rows / height;
        height = _cfg.rows;
    }
    _width = ((int)width) & ~1;
    _height = ((int)height) & ~1;

    SCStreamConfiguration *c = [[SCStreamConfiguration alloc] init];
    c.width = _width;
    c.height = _height;
    c.minimumFrameInterval = CMTimeMake(1, _cfg.fps > 0 ? _cfg.fps : 15);
    c.pixelFormat = kCVPixelFormatType_420YpCbCr8BiPlanarVideoRange;
    c.showsCursor = YES;
    c.queueDepth = 6;
    c.capturesAudio = YES;
    c.sampleRate = 48000;
    c.channelCount = 2;
    c.excludesCurrentProcessAudio = YES;

    _stream = [[SCStream alloc] initWithFilter:filter configuration:c delegate:self];
    NSError *error = nil;
    if (![_stream addStreamOutput:self type:SCStreamOutputTypeScreen sampleHandlerQueue:_queue error:&error] ||
        ![_stream addStreamOutput:self type:SCStreamOutputTypeAudio sampleHandlerQueue:_queue error:&error]) {
        [self end:[NSString stringWithFormat:@"capture couldn't be set up: %@", error.localizedDescription]];
        return;
    }
    [_stream startCaptureWithCompletionHandler:^(NSError *failed) {
        dispatch_async(self->_queue, ^{
            if (failed) {
                [self end:[NSString stringWithFormat:@"capture didn't start: %@", failed.localizedDescription]];
                return;
            }
            // Twice a second, see that the picture is still moving.
            self->_timer = dispatch_source_create(DISPATCH_SOURCE_TYPE_TIMER, 0, 0, self->_queue);
            dispatch_source_set_timer(self->_timer, dispatch_time(DISPATCH_TIME_NOW, NSEC_PER_SEC / 2), NSEC_PER_SEC / 2, NSEC_PER_SEC / 20);
            dispatch_source_set_event_handler(self->_timer, ^{
                [self keepMoving];
            });
            dispatch_resume(self->_timer);
            report(ONAIR_STARTED, [NSString stringWithFormat:@"%d by %d", self->_width, self->_height]);
        });
    }];
}

- (void)stream:(SCStream *)stream didOutputSampleBuffer:(CMSampleBufferRef)samples ofType:(SCStreamOutputType)type {
    if (_ended || !CMSampleBufferIsValid(samples)) return;
    CMTime time = CMSampleBufferGetPresentationTimeStamp(samples);
    if (type == SCStreamOutputTypeAudio) {
        if (!_soundFormat) {
            CMFormatDescriptionRef format = CMSampleBufferGetFormatDescription(samples);
            if (format) _soundFormat = (CMFormatDescriptionRef)CFRetain(format);
        }
        if (_writer.started) [_writer appendSound:samples track:0];
        return;
    }
    if (type != SCStreamOutputTypeScreen) return;
    _clockOffset = CMTimeSubtract(time, CMClockGetTime(CMClockGetHostTimeClock()));
    CFArrayRef attachments = CMSampleBufferGetSampleAttachmentsArray(samples, false);
    NSDictionary *info = attachments && CFArrayGetCount(attachments) > 0 ? (__bridge NSDictionary *)CFArrayGetValueAtIndex(attachments, 0) : nil;
    CVPixelBufferRef pixels = CMSampleBufferGetImageBuffer(samples);
    // Only a complete frame carries a picture. An idle one says the screen
    // didn't change.
    if ([info[SCStreamFrameInfoStatus] integerValue] != SCFrameStatusComplete || !pixels) return;
    if (_last) CVPixelBufferRelease(_last);
    _last = CVPixelBufferRetain(pixels);
    if (CMTIME_IS_INVALID(_firstFrame)) _firstFrame = time;
    [self write:time];
}

// The writer starts with the first frame once the sound's format is known,
// or after a second without sound, which is then left out.
- (void)write:(CMTime)time {
    if (_stopping) return;
    if (!_writer) {
        if (!_soundFormat && CMTimeGetSeconds(CMTimeSubtract(time, _firstFrame)) < 1.0) return;
        OAWriter *writer = [[OAWriter alloc] initWithWidth:_width height:_height fps:_cfg.fps pieceSeconds:_cfg.pieceSeconds bitrate:_cfg.videoBitrate realTime:YES];
        if (_soundFormat && [writer addSoundTrack:_soundFormat] != 0) {
            [self end:@"the writer refused the sound track"];
            return;
        }
        NSError *error = nil;
        if (![writer startAt:time error:&error]) {
            [self end:[NSString stringWithFormat:@"the writer didn't start: %@", error.localizedDescription ?: @"no reason given"]];
            return;
        }
        _writer = writer;
    }
    if ([_writer appendPicture:_last at:time]) {
        _lastWritten = time;
    } else if (_writer.error) {
        [self end:[NSString stringWithFormat:@"writing stopped: %@", _writer.error.localizedDescription]];
    }
}

- (void)keepMoving {
    if (_ended || _stopping || !_last) return;
    CMTime now = CMTimeAdd(CMClockGetTime(CMClockGetHostTimeClock()), _clockOffset);
    if (CMTIME_IS_VALID(_lastWritten) && CMTimeGetSeconds(CMTimeSubtract(now, _lastWritten)) < 1.0) return;
    [self write:now];
}

- (void)stop {
    dispatch_async(_queue, ^{
        if (self->_ended || self->_stopping) return;
        self->_stopping = YES;
        SCStream *stream = self->_stream;
        void (^finish)(void) = ^{
            dispatch_async(self->_queue, ^{
                [self finish:nil];
            });
        };
        if (stream) {
            [stream stopCaptureWithCompletionHandler:^(NSError *error) {
                finish();
            }];
        } else {
            finish();
        }
    });
}

// The stream stopped by itself: the display went away, the permission was
// taken back, or capture failed.
- (void)stream:(SCStream *)stream didStopWithError:(NSError *)error {
    dispatch_async(_queue, ^{
        if (self->_ended || self->_stopping) return;
        self->_stopping = YES;
        [self finish:[NSString stringWithFormat:@"capture stopped: %@", error.localizedDescription ?: @"no reason given"]];
    });
}

// Close the writer, so the last segment is handed over, then say how it ended.
- (void)finish:(NSString *)why {
    if (_ended) return;
    if (_timer) {
        dispatch_source_cancel(_timer);
        _timer = nil;
    }
    OAWriter *writer = _writer;
    if (!writer) {
        [self end:why ?: @"nothing was recorded: no frame arrived"];
        return;
    }
    [writer finish:^(NSError *error) {
        dispatch_async(self->_queue, ^{
            NSString *said = why;
            if (!said && error) said = [NSString stringWithFormat:@"the last piece wasn't written: %@", error.localizedDescription];
            if (said) {
                [self end:said];
                return;
            }
            self->_ended = YES;
            [self letGo];
            report(ONAIR_FINISHED, [NSString stringWithFormat:@"%ld frames and %ld sound buffers were dropped", writer.droppedFrames, writer.droppedSound]);
        });
    }];
}

- (void)end:(NSString *)why {
    if (_ended) return;
    _ended = YES;
    if (_timer) {
        dispatch_source_cancel(_timer);
        _timer = nil;
    }
    [self letGo];
    report(ONAIR_FAILED, why);
}

// Drop what capture held.
- (void)letGo {
    if (_last) {
        CVPixelBufferRelease(_last);
        _last = NULL;
    }
    if (_soundFormat) {
        CFRelease(_soundFormat);
        _soundFormat = NULL;
    }
    _stream = nil;
}

@end

static OACapture *current;

void onair_start(onair_config cfg) {
    dispatch_async(dispatch_get_main_queue(), ^{
        current = [[OACapture alloc] init];
        [current start:cfg];
    });
}

void onair_stop(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        [current stop];
    });
}
