#import "recorder.h"
#import "writer.h"
#import "mixer.h"
#import "choose.h"
#import <AVFoundation/AVFoundation.h>
#import <ScreenCaptureKit/ScreenCaptureKit.h>
#import <CoreGraphics/CoreGraphics.h>
#import "_cgo_export.h"

// OACapture takes picture and sound from ScreenCaptureKit, of the display, the
// area of one or the window Go asks for, with the microphone when it is asked
// for, and gives them to the writer: the picture as it comes, and the two
// sounds mixed into the one sound track the writer takes. It keeps the
// picture moving when the screen is still: a segment ends on a key frame, so
// with no frames no piece would close, and a kill would cost all the sound
// since the last one.
@interface OACapture : NSObject <SCStreamOutput, SCStreamDelegate>
- (void)start:(onair_config)cfg display:(NSString *)display window:(SCContentFilter *)filter id:(CGWindowID)window;
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
    OAMixer *_mixer;
    onair_config _cfg;
    int _width, _height;
    CVPixelBufferRef _last; // the newest whole frame
    CMTime _lastWritten;    // when a frame was last written, on the stream's clock
    CMTime _clockOffset;    // the stream's clock minus the host's
    BOOL _windowed;         // recording a window
    BOOL _stopping, _ended;
}

- (instancetype)init {
    if (!(self = [super init])) return nil;
    _queue = dispatch_queue_create("onair.capture", DISPATCH_QUEUE_SERIAL);
    _lastWritten = kCMTimeInvalid;
    _clockOffset = kCMTimeZero;
    return self;
}

- (void)dealloc {
    if (_last) CVPixelBufferRelease(_last);
}

- (void)start:(onair_config)cfg display:(NSString *)display window:(SCContentFilter *)filter id:(CGWindowID)window {
    _cfg = cfg;
    if (cfg.window) {
        dispatch_async(_queue, ^{
            if (!filter || !OAWindowOpen(window)) {
                [self end:ONAIR_TARGET_GONE why:@"the picked window is gone."];
                return;
            }
            self->_windowed = YES;
            [self microphoneThen:^{
                [self begin:filter size:filter.contentRect.size scale:filter.pointPixelScale area:CGRectNull];
            }];
        });
        return;
    }
    [SCShareableContent getShareableContentExcludingDesktopWindows:NO onScreenWindowsOnly:YES completionHandler:^(SCShareableContent *content, NSError *error) {
        dispatch_async(self->_queue, ^{
            if (error || content.displays.count == 0) {
                // Without the permission macOS answers with an error, having
                // shown its own request the first time.
                [self end:[NSString stringWithFormat:@"On Air can't see the screen: %@. Allow it in System Settings, Privacy & Security, Screen & System Audio Recording, then start again.", error.localizedDescription ?: @"no display was found"]];
                return;
            }
            SCDisplay *chosen = nil;
            for (SCDisplay *d in content.displays) {
                if (display.length ? [OADisplayID(d.displayID) isEqualToString:display] : d.displayID == CGMainDisplayID()) chosen = d;
            }
            if (!chosen && display.length) {
                [self end:ONAIR_TARGET_GONE why:@"the picked display isn't connected."];
                return;
            }
            if (!chosen) chosen = content.displays.firstObject;
            CGRect area = CGRectNull;
            if (self->_cfg.areaWidth > 0 && self->_cfg.areaHeight > 0) {
                area = CGRectMake(self->_cfg.areaX, self->_cfg.areaY, self->_cfg.areaWidth, self->_cfg.areaHeight);
                CGRect bounds = CGRectInset(CGRectMake(0, 0, chosen.width, chosen.height), -0.5, -0.5);
                if (!CGRectContainsRect(bounds, area)) {
                    [self end:ONAIR_TARGET_GONE why:@"the picked area isn't on the display."];
                    return;
                }
            }
            SCContentFilter *shown = [[SCContentFilter alloc] initWithDisplay:chosen excludingWindows:@[]];
            CGSize size = CGRectIsNull(area) ? CGSizeMake(chosen.width, chosen.height) : area.size;
            [self microphoneThen:^{
                [self begin:shown size:size scale:shown.pointPixelScale area:area];
            }];
        });
    }];
}

// MicrophoneThen goes on once the microphone can be recorded, when it is
// asked for, and reports why when it can't.
- (void)microphoneThen:(dispatch_block_t)next {
    if (!_cfg.microphone) {
        next();
        return;
    }
    [self microphone:^(NSString *why) {
        if (why) {
            [self end:ONAIR_MICROPHONE_FAILED why:why];
            return;
        }
        next();
    }];
}

// Then says, on the queue, whether the microphone can be recorded: nil, or
// why not. It has a permission of its own, which macOS asks for once.
- (void)microphone:(void (^)(NSString *why))then {
    NSString *refused = @"On Air isn't allowed to use it. Allow it in System Settings, Privacy & Security, Microphone.";
    AVAuthorizationStatus status = [AVCaptureDevice authorizationStatusForMediaType:AVMediaTypeAudio];
    if (status == AVAuthorizationStatusNotDetermined) {
        [AVCaptureDevice requestAccessForMediaType:AVMediaTypeAudio completionHandler:^(BOOL granted) {
            dispatch_async(self->_queue, ^{
                then(granted ? [self microphoneFound] : refused);
            });
        }];
        return;
    }
    then(status == AVAuthorizationStatusAuthorized ? [self microphoneFound] : refused);
}

- (NSString *)microphoneFound {
    return [AVCaptureDevice defaultDeviceWithMediaType:AVMediaTypeAudio] ? nil : @"no microphone was found.";
}

// Begin captures what filter shows, of a size in points: all of it, or an
// area of a display.
- (void)begin:(SCContentFilter *)filter size:(CGSize)points scale:(double)scale area:(CGRect)area {
    if (_ended) return;
    onair_frame_size(points.width, points.height, scale, _cfg.rows, &_width, &_height);

    SCStreamConfiguration *c = [[SCStreamConfiguration alloc] init];
    c.width = _width;
    c.height = _height;
    if (!CGRectIsNull(area)) c.sourceRect = area;
    if (_windowed) {
        // The picture's size can't change mid-recording. A window that is
        // resized is scaled to fit it, keeping its shape.
        c.scalesToFit = YES;
        c.ignoreShadowsSingleWindow = YES;
    }
    c.minimumFrameInterval = CMTimeMake(1, _cfg.fps > 0 ? _cfg.fps : 15);
    c.pixelFormat = kCVPixelFormatType_420YpCbCr8BiPlanarVideoRange;
    c.showsCursor = YES;
    c.queueDepth = 6;
    c.capturesAudio = YES;
    c.sampleRate = 48000;
    c.channelCount = 2;
    c.excludesCurrentProcessAudio = YES;
    c.captureMicrophone = _cfg.microphone != 0;

    _stream = [[SCStream alloc] initWithFilter:filter configuration:c delegate:self];
    NSError *error = nil;
    if (![_stream addStreamOutput:self type:SCStreamOutputTypeScreen sampleHandlerQueue:_queue error:&error] ||
        ![_stream addStreamOutput:self type:SCStreamOutputTypeAudio sampleHandlerQueue:_queue error:&error]) {
        [self end:[NSString stringWithFormat:@"capture couldn't be set up: %@", error.localizedDescription]];
        return;
    }
    if (_cfg.microphone && ![_stream addStreamOutput:self type:SCStreamOutputTypeMicrophone sampleHandlerQueue:_queue error:&error]) {
        [self end:ONAIR_MICROPHONE_FAILED why:[NSString stringWithFormat:@"its sound couldn't be taken: %@", error.localizedDescription]];
        return;
    }
    [_stream startCaptureWithCompletionHandler:^(NSError *failed) {
        dispatch_async(self->_queue, ^{
            if (failed && self->_cfg.microphone) {
                // Go hears that the microphone was asked for, and decides
                // what comes next.
                [self end:ONAIR_MICROPHONE_FAILED why:[NSString stringWithFormat:@"capture didn't start with it: %@", failed.localizedDescription]];
                return;
            }
            if (failed) {
                [self end:[NSString stringWithFormat:@"capture didn't start: %@", failed.localizedDescription]];
                return;
            }
            // Twice a second, see that the picture and the sound keep moving.
            self->_timer = dispatch_source_create(DISPATCH_SOURCE_TYPE_TIMER, 0, 0, self->_queue);
            dispatch_source_set_timer(self->_timer, dispatch_time(DISPATCH_TIME_NOW, NSEC_PER_SEC / 2), NSEC_PER_SEC / 2, NSEC_PER_SEC / 20);
            dispatch_source_set_event_handler(self->_timer, ^{
                [self keepMoving];
            });
            dispatch_resume(self->_timer);
            report(ONAIR_STARTED, [NSString stringWithFormat:@"%d by %d%@", self->_width, self->_height, self->_cfg.microphone ? @", with the microphone" : @""]);
        });
    }];
}

- (void)stream:(SCStream *)stream didOutputSampleBuffer:(CMSampleBufferRef)samples ofType:(SCStreamOutputType)type {
    if (_ended || !CMSampleBufferIsValid(samples)) return;
    if (type == SCStreamOutputTypeAudio || type == SCStreamOutputTypeMicrophone) {
        // Sound before the first whole frame has no timeline yet, and is
        // left out.
        [_mixer add:samples source:type == SCStreamOutputTypeAudio ? 0 : 1];
        return;
    }
    if (type != SCStreamOutputTypeScreen) return;
    CMTime time = CMSampleBufferGetPresentationTimeStamp(samples);
    _clockOffset = CMTimeSubtract(time, CMClockGetTime(CMClockGetHostTimeClock()));
    CFArrayRef attachments = CMSampleBufferGetSampleAttachmentsArray(samples, false);
    NSDictionary *info = attachments && CFArrayGetCount(attachments) > 0 ? (__bridge NSDictionary *)CFArrayGetValueAtIndex(attachments, 0) : nil;
    CVPixelBufferRef pixels = CMSampleBufferGetImageBuffer(samples);
    // Only a complete frame carries a picture. An idle one says the screen
    // didn't change.
    if ([info[SCStreamFrameInfoStatus] integerValue] != SCFrameStatusComplete || !pixels) return;
    if (_last) CVPixelBufferRelease(_last);
    _last = CVPixelBufferRetain(pixels);
    [self write:time];
}

// The writer starts with the first whole frame. Its one sound track takes
// what the mixer hands on, from the same moment, so it needn't wait to learn
// the sound's format.
- (void)write:(CMTime)time {
    if (_stopping) return;
    if (!_writer) {
        OAWriter *writer = [[OAWriter alloc] initWithWidth:_width height:_height fps:_cfg.fps pieceSeconds:_cfg.pieceSeconds bitrate:_cfg.videoBitrate realTime:YES];
        OAMixer *mixer = [[OAMixer alloc] initWithSources:_cfg.microphone ? 2 : 1 sink:^(CMSampleBufferRef samples) {
            [writer appendSound:samples track:0];
        }];
        if (!mixer || [writer addSoundTrack:mixer.format] != 0) {
            [self end:@"the writer refused the sound track"];
            return;
        }
        NSError *error = nil;
        if (![writer startAt:time error:&error]) {
            [self end:[NSString stringWithFormat:@"the writer didn't start: %@", error.localizedDescription ?: @"no reason given"]];
            return;
        }
        [mixer startAt:time];
        _writer = writer;
        _mixer = mixer;
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
    // Sound keeps pace with the clock, silent where none came, so the track
    // has no hole when nothing plays.
    [_mixer advanceTo:now];
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

// Every window being recorded has gone. For a window's recording, that is
// the window closing, and Go decides what follows.
- (void)streamDidBecomeInactive:(SCStream *)stream {
    dispatch_async(_queue, ^{
        if (self->_ended || self->_stopping || !self->_windowed) return;
        report(ONAIR_WINDOW_CLOSED, @"");
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
    // Capture has stopped. The sound the mixer still holds goes in before
    // the writer closes, and nothing reaches the mixer after.
    OAMixer *mixer = _mixer;
    [mixer flush];
    _mixer = nil;
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
            report(ONAIR_FINISHED, [NSString stringWithFormat:@"%ld frames and %ld sound buffers were dropped, and %ld frames of sound came too late or too early to mix", writer.droppedFrames, writer.droppedSound, mixer.late + mixer.early]);
        });
    }];
}

- (void)end:(NSString *)why {
    [self end:ONAIR_FAILED why:why];
}

- (void)end:(int)code why:(NSString *)why {
    if (_ended) return;
    _ended = YES;
    if (_timer) {
        dispatch_source_cancel(_timer);
        _timer = nil;
    }
    [self letGo];
    report(code, why);
}

// Drop what capture held.
- (void)letGo {
    if (_last) {
        CVPixelBufferRelease(_last);
        _last = NULL;
    }
    _mixer = nil;
    _stream = nil;
}

@end

void onair_frame_size(double width, double height, double scale, int rows, int *wide, int *high) {
    if (scale <= 0) scale = 1;
    double w = width * scale, h = height * scale;
    if (rows > 0 && h > rows) {
        w = w * rows / h;
        h = rows;
    }
    // Even sides, as the encoder wants, and never none.
    *wide = MAX(((int)w) & ~1, 2);
    *high = MAX(((int)h) & ~1, 2);
}

static OACapture *current;

void onair_start(onair_config cfg) {
    // The display's name is Go's, and gone once this returns.
    NSString *display = [NSString stringWithUTF8String:cfg.display ?: ""];
    cfg.display = NULL;
    dispatch_async(dispatch_get_main_queue(), ^{
        CGWindowID window = 0;
        SCContentFilter *filter = cfg.window ? OAPickedWindow(&window) : nil;
        current = [[OACapture alloc] init];
        [current start:cfg display:display window:filter id:window];
    });
}

void onair_stop(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        [current stop];
    });
}
