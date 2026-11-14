#import "recorder.h"
#import "live.h"
#import "panel.h"
#import <AVFoundation/AVFoundation.h>
#import <Speech/Speech.h>
#import <stdatomic.h>
#import "_cgo_export.h"

// The live side's part of the Mac: speech recognition of capture's two
// sounds, on the Mac itself, and the panel. It decides nothing: Go says
// when to listen and what the panel shows, and hears what was said and
// what went wrong. Its work runs on a queue of its own, and capture's
// queue only copies a buffer, while listening, and hands it on.

static void say(int code, NSString *detail) {
    onairLive(code, (char *)(detail.UTF8String ?: ""));
}

// Buffers that may wait for recognition at once, from both sounds. Past
// that, capture's buffers are dropped rather than held.
static const int mostWaiting = 512;

static atomic_bool listening;   // capture copies its sound only while set
static atomic_int waiting;      // copies on their way to the listener
static atomic_long dropped;     // copies dropped because too many waited
static atomic_long generation;  // which listener a copy is for

static dispatch_queue_t OALiveQueue(void) {
    static dispatch_queue_t queue;
    static dispatch_once_t once;
    dispatch_once(&once, ^{
        queue = dispatch_queue_create("onair.live", dispatch_queue_attr_make_with_qos_class(DISPATCH_QUEUE_SERIAL, QOS_CLASS_UTILITY, 0));
    });
    return queue;
}

static double OANow(void) {
    return [[NSDate date] timeIntervalSince1970];
}

// Copy takes a buffer's sound out of capture's buffer, into one of its own.
static AVAudioPCMBuffer *OACopySound(CMSampleBufferRef samples) {
    CMAudioFormatDescriptionRef description = CMSampleBufferGetFormatDescription(samples);
    AVAudioFrameCount frames = (AVAudioFrameCount)CMSampleBufferGetNumSamples(samples);
    if (!description || frames == 0) return nil;
    AVAudioFormat *format = [[AVAudioFormat alloc] initWithCMAudioFormatDescription:description];
    if (!format || format.sampleRate <= 0) return nil;
    AVAudioPCMBuffer *copy = [[AVAudioPCMBuffer alloc] initWithPCMFormat:format frameCapacity:frames];
    if (!copy) return nil;
    copy.frameLength = frames;
    if (CMSampleBufferCopyPCMDataIntoAudioBufferList(samples, 0, (int32_t)frames, copy.mutableAudioBufferList) != noErr) return nil;
    return copy;
}

// What recognition reports that isn't a problem: no speech in a request,
// and a request that was called off or ended.
static BOOL OAQuietEnd(NSError *error) {
    if ([error.domain isEqualToString:@"kAFAssistantErrorDomain"]) return error.code == 1110 || error.code == 216 || error.code == 301 || error.code == 203;
    return [error.domain isEqualToString:NSCocoaErrorDomain] && error.code == NSUserCancelledError;
}

static NSString *OARefusal(SFSpeechRecognizerAuthorizationStatus status) {
    switch (status) {
    case SFSpeechRecognizerAuthorizationStatusDenied:
        return @"it was refused";
    case SFSpeechRecognizerAuthorizationStatusRestricted:
        return @"this Mac restricts it";
    default:
        return @"no answer was given";
    }
}

// CanAsk says whether macOS can be asked about speech recognition: only
// the app that tools/build-app makes says why it wants it, and macOS ends
// a process that asks without saying.
static BOOL OACanAsk(void) {
    return [NSBundle.mainBundle objectForInfoDictionaryKey:@"NSSpeechRecognitionUsageDescription"] != nil;
}

// OAVoice recognizes one of the two sounds: a request at a time, each
// ended after chunk seconds, so that its finished words come that often.
@interface OAVoice : NSObject
@property int speaker;
@property(strong) SFSpeechRecognizer *recognizer;
@property(strong) SFSpeechAudioBufferRecognitionRequest *request;
@property(strong) SFSpeechRecognitionTask *task;
@property(strong) AVAudioConverter *converter;
@property(strong) AVAudioFormat *from;
@property double opened; // when the request was made, on the clock
@end

@implementation OAVoice
@end

// OAListener listens while a recording runs with Live Context on. It is
// used on the live queue only.
@interface OAListener : NSObject
- (instancetype)initWithChunk:(double)chunk;
- (void)start;
- (void)hear:(AVAudioPCMBuffer *)buffer sound:(int)sound at:(double)wall;
- (void)tick;
- (void)stop;
@end

@implementation OAListener {
    double _chunk;
    BOOL _ready, _stopped;
    NSArray<OAVoice *> *_voices; // the call's sound, then the microphone
    NSMutableArray<SFSpeechRecognitionTask *> *_ending; // ended requests, until their words come
    NSOperationQueue *_results;
    AVAudioFormat *_format; // what recognition is given
    long _droppedSaid;
    NSMutableSet<NSString *> *_said; // problems already reported
}

- (instancetype)initWithChunk:(double)chunk {
    if (!(self = [super init])) return nil;
    _chunk = chunk > 1 ? chunk : 15;
    _ending = [NSMutableArray array];
    _said = [NSMutableSet set];
    _results = [[NSOperationQueue alloc] init];
    _results.maxConcurrentOperationCount = 1;
    _results.qualityOfService = NSQualityOfServiceUtility;
    _droppedSaid = atomic_load(&dropped);
    return self;
}

// Start asks macOS, when it hasn't been asked yet, and gets ready once
// recognition is allowed and can run on the Mac.
- (void)start {
    if (!OACanAsk()) {
        say(ONAIR_LIVE_REFUSED, @"this copy of On Air isn't the app tools/build-app makes, so macOS can't be asked");
        return;
    }
    SFSpeechRecognizerAuthorizationStatus status = [SFSpeechRecognizer authorizationStatus];
    if (status == SFSpeechRecognizerAuthorizationStatusNotDetermined) {
        [SFSpeechRecognizer requestAuthorization:^(SFSpeechRecognizerAuthorizationStatus answer) {
            dispatch_async(OALiveQueue(), ^{
                if (answer == SFSpeechRecognizerAuthorizationStatusAuthorized) {
                    [self ready];
                } else if (!self->_stopped) {
                    say(ONAIR_LIVE_REFUSED, OARefusal(answer));
                }
            });
        }];
        return;
    }
    if (status != SFSpeechRecognizerAuthorizationStatusAuthorized) {
        say(ONAIR_LIVE_REFUSED, OARefusal(status));
        return;
    }
    [self ready];
}

- (void)ready {
    if (_stopped || _ready) return;
    SFSpeechRecognizer *first = [[SFSpeechRecognizer alloc] init];
    NSLocale *locale = first.locale ?: NSLocale.currentLocale;
    NSString *language = [NSLocale.currentLocale localizedStringForLocaleIdentifier:locale.localeIdentifier] ?: locale.localeIdentifier;
    // Without the Mac's own recognition for the language, nothing is
    // recognized: the call's words never go to Apple's servers.
    if (!first || !first.supportsOnDeviceRecognition) {
        say(ONAIR_LIVE_UNSUPPORTED, language);
        return;
    }
    SFSpeechRecognizer *second = [[SFSpeechRecognizer alloc] initWithLocale:locale];
    if (!second || !second.supportsOnDeviceRecognition) {
        say(ONAIR_LIVE_UNSUPPORTED, language);
        return;
    }
    AVAudioFormat *format = [[SFSpeechAudioBufferRecognitionRequest alloc] init].nativeAudioFormat;
    if (!format || format.sampleRate <= 0 || format.channelCount < 1) {
        format = [[AVAudioFormat alloc] initWithCommonFormat:AVAudioPCMFormatFloat32 sampleRate:16000 channels:1 interleaved:NO];
    }
    _format = format;
    OAVoice *call = [[OAVoice alloc] init], *person = [[OAVoice alloc] init];
    call.speaker = ONAIR_SPEAKER_OTHERS;
    call.recognizer = first;
    person.speaker = ONAIR_SPEAKER_PERSON;
    person.recognizer = second;
    for (OAVoice *voice in @[ call, person ]) voice.recognizer.queue = _results;
    _voices = @[ call, person ];
    _ready = YES;
    say(ONAIR_LIVE_ALLOWED, @"");
    atomic_store(&listening, true);
}

- (void)hear:(AVAudioPCMBuffer *)buffer sound:(int)sound at:(double)wall {
    if (!_ready || _stopped || sound < 0 || sound >= (int)_voices.count) return;
    OAVoice *voice = _voices[sound];
    if (voice.request && wall - voice.opened >= _chunk) [self end:voice];
    AVAudioPCMBuffer *converted = [self convert:buffer for:voice];
    if (!converted.frameLength) return;
    if (!voice.request) [self begin:voice at:wall - buffer.frameLength / buffer.format.sampleRate];
    [voice.request appendAudioPCMBuffer:converted];
}

// Convert brings a buffer to the format recognition is given: one channel,
// at its rate. A converter is kept for each sound while its format holds.
- (AVAudioPCMBuffer *)convert:(AVAudioPCMBuffer *)buffer for:(OAVoice *)voice {
    if (!voice.converter || ![voice.from isEqual:buffer.format]) {
        voice.from = buffer.format;
        voice.converter = [[AVAudioConverter alloc] initFromFormat:buffer.format toFormat:_format];
        voice.converter.downmix = YES;
        if (!voice.converter) {
            [self problem:[NSString stringWithFormat:@"its sound, %@, can't be converted for recognition", buffer.format]];
            return nil;
        }
    }
    AVAudioFrameCount room = (AVAudioFrameCount)ceil(buffer.frameLength * _format.sampleRate / buffer.format.sampleRate) + 64;
    AVAudioPCMBuffer *out = [[AVAudioPCMBuffer alloc] initWithPCMFormat:_format frameCapacity:room];
    if (!out) return nil;
    __block BOOL given = NO;
    NSError *error = nil;
    AVAudioConverterOutputStatus status = [voice.converter convertToBuffer:out error:&error withInputFromBlock:^AVAudioBuffer *(AVAudioPacketCount count, AVAudioConverterInputStatus *inputStatus) {
        if (given) {
            *inputStatus = AVAudioConverterInputStatus_NoDataNow;
            return nil;
        }
        given = YES;
        *inputStatus = AVAudioConverterInputStatus_HaveData;
        return buffer;
    }];
    if (status == AVAudioConverterOutputStatus_Error) {
        [self problem:[NSString stringWithFormat:@"its sound couldn't be converted: %@", error.localizedDescription ?: @"no reason given"]];
        voice.converter = nil;
        return nil;
    }
    return out;
}

// Begin makes a request, on the Mac only, whose finished words go to Go
// with when they were said: began is when its first sound was heard, on
// the clock.
- (void)begin:(OAVoice *)voice at:(double)began {
    SFSpeechAudioBufferRecognitionRequest *request = [[SFSpeechAudioBufferRecognitionRequest alloc] init];
    request.requiresOnDeviceRecognition = YES;
    request.shouldReportPartialResults = NO;
    int speaker = voice.speaker;
    voice.request = request;
    voice.opened = OANow();
    voice.task = [voice.recognizer recognitionTaskWithRequest:request resultHandler:^(SFSpeechRecognitionResult *result, NSError *error) {
        if (result.isFinal) {
            SFTranscription *best = result.bestTranscription;
            SFTranscriptionSegment *first = best.segments.firstObject, *last = best.segments.lastObject;
            if (best.formattedString.length && first && last) {
                onairHeard(speaker, (char *)best.formattedString.UTF8String, began + first.timestamp, began + last.timestamp + last.duration);
            }
        }
        if (error && !OAQuietEnd(error)) {
            NSString *what = [NSString stringWithFormat:@"%@ (%@ %ld)", error.localizedDescription ?: @"no reason given", error.domain, (long)error.code];
            dispatch_async(OALiveQueue(), ^{
                [self problem:what];
            });
        }
    }];
}

// End ends a request's sound, so its finished words come. Its task is kept
// a while, until they do.
- (void)end:(OAVoice *)voice {
    [voice.request endAudio];
    if (voice.task) [_ending addObject:voice.task];
    while (_ending.count > 8) [_ending removeObjectAtIndex:0];
    voice.request = nil;
    voice.task = nil;
}

// Tick, every second, ends a request that has run its time even when no
// sound comes, and says when sound was dropped.
- (void)tick {
    if (!_ready || _stopped) return;
    double now = OANow();
    for (OAVoice *voice in _voices) {
        if (voice.request && now - voice.opened >= _chunk) [self end:voice];
    }
    long lost = atomic_load(&dropped);
    if (lost > _droppedSaid) {
        _droppedSaid = lost;
        say(ONAIR_LIVE_DROPPED, [NSString stringWithFormat:@"%ld", lost]);
    }
}

// Problem tells Go what went wrong with recognition, once each.
- (void)problem:(NSString *)what {
    if (_stopped || [_said containsObject:what] || _said.count > 20) return;
    [_said addObject:what];
    say(ONAIR_LIVE_FAILED, what);
}

- (void)stop {
    _stopped = YES;
    for (OAVoice *voice in _voices) {
        [voice.task cancel];
        voice.request = nil;
        voice.task = nil;
    }
    for (SFSpeechRecognitionTask *task in _ending) [task cancel];
    [_ending removeAllObjects];
}

@end

static OAListener *listener; // on the live queue only
static dispatch_source_t ticker;

void OALiveHear(CMSampleBufferRef samples, int sound) {
    if (!atomic_load(&listening)) return;
    if (atomic_fetch_add(&waiting, 1) >= mostWaiting) {
        atomic_fetch_sub(&waiting, 1);
        atomic_fetch_add(&dropped, 1);
        return;
    }
    AVAudioPCMBuffer *copy = nil;
    @try {
        copy = OACopySound(samples);
    } @catch (NSException *exception) {
        copy = nil;
    }
    if (!copy) {
        atomic_fetch_sub(&waiting, 1);
        return;
    }
    double wall = OANow();
    long mine = atomic_load(&generation);
    dispatch_async(OALiveQueue(), ^{
        atomic_fetch_sub(&waiting, 1);
        if (mine != atomic_load(&generation)) return;
        @try {
            [listener hear:copy sound:sound at:wall];
        } @catch (NSException *exception) {
            say(ONAIR_LIVE_FAILED, [NSString stringWithFormat:@"recognition stopped: %@", exception.reason ?: exception.name]);
            [listener stop];
            listener = nil;
            atomic_store(&listening, false);
        }
    });
}

void onair_live_listen(int on, double chunk) {
    dispatch_async(OALiveQueue(), ^{
        atomic_store(&listening, false);
        atomic_fetch_add(&generation, 1);
        @try {
            [listener stop];
        } @catch (NSException *exception) {
        }
        listener = nil;
        if (ticker) {
            dispatch_source_cancel(ticker);
            ticker = nil;
        }
        if (!on) return;
        @try {
            listener = [[OAListener alloc] initWithChunk:chunk];
            [listener start];
            ticker = dispatch_source_create(DISPATCH_SOURCE_TYPE_TIMER, 0, 0, OALiveQueue());
            dispatch_source_set_timer(ticker, dispatch_time(DISPATCH_TIME_NOW, NSEC_PER_SEC), NSEC_PER_SEC, NSEC_PER_SEC / 10);
            dispatch_source_set_event_handler(ticker, ^{
                @try {
                    [listener tick];
                } @catch (NSException *exception) {
                }
            });
            dispatch_resume(ticker);
        } @catch (NSException *exception) {
            say(ONAIR_LIVE_FAILED, [NSString stringWithFormat:@"recognition didn't start: %@", exception.reason ?: exception.name]);
            listener = nil;
            atomic_store(&listening, false);
        }
    });
}

void onair_live_ask(void) {
    dispatch_async(OALiveQueue(), ^{
        if (!OACanAsk()) {
            say(ONAIR_LIVE_REFUSED, @"this copy of On Air isn't the app tools/build-app makes, so macOS can't be asked");
            return;
        }
        SFSpeechRecognizerAuthorizationStatus status = [SFSpeechRecognizer authorizationStatus];
        if (status == SFSpeechRecognizerAuthorizationStatusNotDetermined) {
            [SFSpeechRecognizer requestAuthorization:^(SFSpeechRecognizerAuthorizationStatus answer) {
                say(answer == SFSpeechRecognizerAuthorizationStatusAuthorized ? ONAIR_LIVE_ALLOWED : ONAIR_LIVE_REFUSED, answer == SFSpeechRecognizerAuthorizationStatusAuthorized ? @"" : OARefusal(answer));
            }];
            return;
        }
        say(status == SFSpeechRecognizerAuthorizationStatusAuthorized ? ONAIR_LIVE_ALLOWED : ONAIR_LIVE_REFUSED, status == SFSpeechRecognizerAuthorizationStatusAuthorized ? @"" : OARefusal(status));
    });
}

// Go calls onair_live_show from one goroutine, in order, and each call
// carries all the panel shows, so one that arrives after a newer one is
// dropped.
static atomic_long panelsAsked;
static long panelsDone; // on the main thread

void onair_live_show(int visible, const char *display, const char *status, const onair_link *links, int count) {
    NSMutableArray<NSDictionary<NSString *, NSString *> *> *list = [NSMutableArray array];
    for (int i = 0; i < count; i++) {
        [list addObject:@{
            @"kind" : [NSString stringWithUTF8String:links[i].kind ?: ""] ?: @"",
            @"title" : [NSString stringWithUTF8String:links[i].title ?: ""] ?: @"",
            @"date" : [NSString stringWithUTF8String:links[i].date ?: ""] ?: @"",
            @"why" : [NSString stringWithUTF8String:links[i].why ?: ""] ?: @"",
            @"link" : [NSString stringWithUTF8String:links[i].link ?: ""] ?: @"",
        }];
    }
    NSString *d = [NSString stringWithUTF8String:display ?: ""] ?: @"", *s = [NSString stringWithUTF8String:status ?: ""] ?: @"";
    long asked = atomic_fetch_add(&panelsAsked, 1) + 1;
    dispatch_async(dispatch_get_main_queue(), ^{
        if (asked <= panelsDone) return;
        panelsDone = asked;
        @try {
            OAPanelShow(visible != 0, d, s, list);
        } @catch (NSException *exception) {
        }
    });
}
