#import "recorder.h"
#import "live.h"
#import "panel.h"
#import "speechmix.h"
#import <AVFoundation/AVFoundation.h>
#import <Speech/Speech.h>
#import <stdatomic.h>
#import "_cgo_export.h"

// The live side's part of the Mac: speech recognition of capture's two
// sounds, on the Mac itself, and the panel. It decides nothing: Go says
// when to listen and what the panel shows, and hears what was said, how
// loud each sound was meanwhile, and what went wrong. Its work runs on a
// queue of its own, and capture's queue only copies a buffer, while
// listening, and hands it on.
//
// Only one recognition runs at a time in a process: a second one ends at
// once, saying it heard no speech. So the two sounds are added into one,
// which one recognizer hears a request at a time, and each sound's loudness
// is measured before they are added, for Go to tell whose each word was.

static void say(int code, NSString *detail) {
    onairLive(code, (char *)(detail.UTF8String ?: ""));
}

// Diag adds a line to the diagnostic log, which Go keeps. It says where
// words go missing in counts, formats and codes, never in words that were
// said.
static void OADiag(NSString *format, ...) NS_FORMAT_FUNCTION(1, 2);
static void OADiag(NSString *format, ...) {
    va_list args;
    va_start(args, format);
    NSString *line = [[NSString alloc] initWithFormat:format arguments:args];
    va_end(args);
    say(ONAIR_LIVE_DIAG, line);
}

static NSString *OAFormatName(AVAudioFormat *format) {
    if (!format) return @"no format";
    NSString *common;
    switch (format.commonFormat) {
    case AVAudioPCMFormatFloat32:
        common = @"float32";
        break;
    case AVAudioPCMFormatFloat64:
        common = @"float64";
        break;
    case AVAudioPCMFormatInt16:
        common = @"int16";
        break;
    case AVAudioPCMFormatInt32:
        common = @"int32";
        break;
    default:
        common = [NSString stringWithFormat:@"format %u", (unsigned)format.streamDescription->mFormatID];
        break;
    }
    return [NSString stringWithFormat:@"%.0f Hz, %u ch, %@, %@", format.sampleRate, (unsigned)format.channelCount, common, format.interleaved ? @"interleaved" : @"planar"];
}

// Peak is a buffer's loudest sample, from 0 to 1 of full scale, or -1 when
// its samples are in a format it doesn't read.
static double OAPeak(AVAudioPCMBuffer *buffer) {
    const AudioBufferList *list = buffer.audioBufferList;
    AVAudioCommonFormat common = buffer.format.commonFormat;
    if (!list || (common != AVAudioPCMFormatFloat32 && common != AVAudioPCMFormatInt16)) return -1;
    double peak = 0;
    for (UInt32 b = 0; b < list->mNumberBuffers; b++) {
        const void *data = list->mBuffers[b].mData;
        size_t bytes = list->mBuffers[b].mDataByteSize;
        if (!data) continue;
        if (common == AVAudioPCMFormatFloat32) {
            for (size_t i = 0; i < bytes / sizeof(float); i++) peak = fmax(peak, fabs((double)((const float *)data)[i]));
        } else {
            for (size_t i = 0; i < bytes / sizeof(int16_t); i++) peak = fmax(peak, abs((int)((const int16_t *)data)[i]) / 32768.0);
        }
    }
    return peak;
}

static NSString *OADecibels(double peak) {
    if (peak < 0) return @"none";
    if (peak == 0) return @"silent";
    return [NSString stringWithFormat:@"%.0f dBFS", 20 * log10(peak)];
}

static long OAWords(NSString *text) {
    long n = 0;
    for (NSString *part in [text componentsSeparatedByCharactersInSet:NSCharacterSet.whitespaceAndNewlineCharacterSet]) {
        if (part.length) n++;
    }
    return n;
}

static const char *OAAuthName(SFSpeechRecognizerAuthorizationStatus status) {
    switch (status) {
    case SFSpeechRecognizerAuthorizationStatusNotDetermined:
        return "not asked yet";
    case SFSpeechRecognizerAuthorizationStatusDenied:
        return "denied";
    case SFSpeechRecognizerAuthorizationStatusRestricted:
        return "restricted";
    case SFSpeechRecognizerAuthorizationStatusAuthorized:
        return "authorized";
    }
    return "unknown";
}

static NSString *OARecognizerName(SFSpeechRecognizer *recognizer) {
    if (!recognizer) return @"none for this language";
    return [NSString stringWithFormat:@"%@, available %@, on the Mac %@", recognizer.locale.localeIdentifier, recognizer.isAvailable ? @"yes" : @"no",
                                      recognizer.supportsOnDeviceRecognition ? @"yes" : @"no"];
}

// What the diagnostic log counts for each sound, on the live queue.
enum {
    OA_BUFFERS,    // buffers that reached the listener
    OA_FRAMES_IN,  // their frames
    OA_CONVERTERS, // converters made
    OA_CONVERTED,  // frames that came out of conversion
    OA_EMPTY,      // conversions that gave none
    OA_SOUND_COUNTS,
};

// And for the mix and recognition's requests.
enum {
    OA_STEPS,       // steps of the mix handed on
    OA_DROPPED,     // steps dropped because too many were held
    OA_BEGUN,       // requests begun
    OA_ENDED,       // requests whose sound was ended
    OA_EARLY,       // requests that ended before their sound did
    OA_CALLED_OFF,  // requests On Air called off because their final didn't come
    OA_CALLS,       // calls of a request's result handler
    OA_PARTIALS,    // results that weren't final
    OA_UPDATES,     // partial results, and how far the sound has gone since, handed to Go
    OA_FINALS,      // final results
    OA_EMPTY_FINALS, // final results with no words
    OA_WORDS,       // words in final results
    OA_HANDED,      // final results handed to Go
    OA_ERRORS,      // errors, normal ends included
    OA_SHOWN,       // errors the panel was told of
    OA_COUNTS,
};

enum {
    kHistory = 1200,  // steps of each sound's loudness kept: two minutes
    kHeld = 300,      // steps of the mix held for recognition at most: thirty seconds
    kMixSeconds = 4,  // how far ahead of what it handed on the mix holds sound
};

// How long a request's final words are waited for after its sound ended,
// and then for it to go once it is called off, in seconds; and how often
// the listener looks at the clock.
static const double kFinalWait = 5, kGoneWait = 1, kTick = 0.25;

// Steps of sound a request is given after a partial result before Go is
// told how far its sound has gone, so that it can tell a pause.
static const long kUpdateSteps = 2;

// Buffers of each sound that capture offered while listening, and those of
// them that couldn't be copied, counted on capture's queue.
static atomic_long offered[2], uncopied[2];

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

// Ending says whether an error is one recognition ends a request with: no
// speech in its sound, or the request called off. Either is a normal end
// once the request's final words came, its sound ended or On Air called it
// off, and a problem before.
static BOOL OAEnding(NSError *error) {
    if ([error.domain isEqualToString:@"kAFAssistantErrorDomain"]) return error.code == 1110 || error.code == 216 || error.code == 301 || error.code == 203;
    if ([error.domain isEqualToString:@"kLSRErrorDomain"]) return error.code == 301;
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

// OASound is one of capture's two sounds, on its way into the mix: brought
// to one channel at recognition's rate by a converter kept while its format
// holds. It is used on the live queue only.
@interface OASound : NSObject {
  @public
    long count[OA_SOUND_COUNTS]; // for the diagnostic log
    double peak;                 // its loudest sample since the last count, or -1
}
@property(copy) NSString *name; // as the diagnostic log names it
@property(strong) AVAudioConverter *converter;
@property(strong) AVAudioFormat *from;
@end

@implementation OASound
@end

// OARequest is one request to recognition: its number, its task, the step
// of the mix its sound began at and how many steps it was given, and how
// it is ending, on the clock. It is used on the live queue only.
@interface OARequest : NSObject
@property long number, start, steps;
@property long handed;                     // steps it had been given when Go was last told of it
@property long partials, updates;          // its partial results, and those and updates handed to Go
@property(copy) NSString *text;            // its newest partial result's words
@property(copy) NSArray<SFTranscriptionSegment *> *segments; // and their segments
@property(strong) SFSpeechAudioBufferRecognitionRequest *request;
@property(strong) SFSpeechRecognitionTask *task;
@property double begun, ended, calledOff; // 0 until then
@property BOOL final, done;               // its final came; its final or an error came
@end

@implementation OARequest
@end

// OAListener listens while a recording runs with Live Context on. It is
// used on the live queue only.
@interface OAListener : NSObject
- (instancetype)initWithChunk:(double)chunk;
- (void)start;
- (void)hear:(AVAudioPCMBuffer *)buffer sound:(int)sound stamp:(CMTime)stamp at:(double)wall;
- (void)tick;
- (void)stop;
@end

static OAListener *listener; // on the live queue only
static dispatch_source_t ticker;

// Fail ends listening after an exception in the live side's own work, which
// would otherwise end On Air, and the recording with it. One from a
// listener that has been replaced stops only that one.
static void OAFail(OAListener *who, NSException *exception) {
    @try {
        [who stop];
    } @catch (NSException *again) {
    }
    if (who != listener) return;
    say(ONAIR_LIVE_FAILED, [NSString stringWithFormat:@"recognition stopped: %@", exception.reason ?: exception.name]);
    listener = nil;
    atomic_store(&listening, false);
    if (ticker) {
        dispatch_source_cancel(ticker);
        ticker = nil;
    }
}

@implementation OAListener {
    double _chunk;
    long _chunkSteps; // steps a request is given before its sound ends
    long _ticks;
    BOOL _ready, _stopped;
    SFSpeechRecognizer *_recognizer;
    NSOperationQueue *_results;
    AVAudioFormat *_format;        // what each sound is brought to, and recognition given
    NSArray<OASound *> *_sounds;   // the call's, then the microphone's
    oa_speechmix _mix;
    BOOL _mixing;                  // the mix was made
    BOOL _started;                 // the mix's timeline has its start
    CMTime _origin;                // its first frame, on the stamps' clock
    double _originWall;            // and on the clock
    double _placedWall;            // when sound was last placed in it
    float _loud[kHistory][2];      // each sound's loudness in the last steps
    long _loudStep[kHistory];      // which step each holds
    NSMutableArray<AVAudioPCMBuffer *> *_held; // steps of the mix not yet given to recognition, in order
    long _heldFirst;               // the first one's step
    long _heldMost;                // the most held at once
    OARequest *_current;           // the request recognition is given, or is finishing
    double _notBefore;             // when the next request may begin
    double _settled;               // when the last request's final or error came
    long _count[OA_COUNTS];
    double _peak;                  // the mix's loudest sample since the last count, or -1
    long _droppedSaid;
    NSMutableSet<NSString *> *_said; // problems already reported
}

- (instancetype)initWithChunk:(double)chunk {
    if (!(self = [super init])) return nil;
    _chunk = chunk > 1 ? chunk : 15;
    _held = [NSMutableArray array];
    _said = [NSMutableSet set];
    _results = [[NSOperationQueue alloc] init];
    _results.maxConcurrentOperationCount = 1;
    _results.qualityOfService = NSQualityOfServiceUtility;
    _droppedSaid = atomic_load(&dropped);
    _peak = -1;
    for (int i = 0; i < kHistory; i++) _loudStep[i] = -1;
    return self;
}

- (void)dealloc {
    if (_mixing) oa_speechmix_free(&_mix);
}

// Start asks macOS, when it hasn't been asked yet, and gets ready once
// recognition is allowed and can run on the Mac.
- (void)start {
    if (!OACanAsk()) {
        OADiag(@"listening starts, but this copy can't ask macOS about speech recognition");
        say(ONAIR_LIVE_REFUSED, @"this copy of On Air isn't the app tools/build-app makes, so macOS can't be asked");
        return;
    }
    SFSpeechRecognizerAuthorizationStatus status = [SFSpeechRecognizer authorizationStatus];
    OADiag(@"listening starts: speech recognition %s, both sounds as one, a request every %.0f s of sound, its partial results handed on as they come", OAAuthName(status), _chunk);
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
    SFSpeechRecognizer *recognizer = [[SFSpeechRecognizer alloc] init];
    NSLocale *locale = recognizer.locale ?: NSLocale.currentLocale;
    NSString *language = [NSLocale.currentLocale localizedStringForLocaleIdentifier:locale.localeIdentifier] ?: locale.localeIdentifier;
    OADiag(@"recognizer: %@", OARecognizerName(recognizer));
    // Without the Mac's own recognition for the language, nothing is
    // recognized: the call's words never go to Apple's servers.
    if (!recognizer || !recognizer.supportsOnDeviceRecognition) {
        say(ONAIR_LIVE_UNSUPPORTED, language);
        return;
    }
    // Recognition is given floats in one channel, at the rate it names.
    AVAudioFormat *native = [[SFSpeechAudioBufferRecognitionRequest alloc] init].nativeAudioFormat;
    double rate = native.sampleRate >= 8000 && native.sampleRate <= 48000 ? native.sampleRate : 16000;
    _format = [[AVAudioFormat alloc] initStandardFormatWithSampleRate:rate channels:1];
    if (!_format || oa_speechmix_init(&_mix, lround(rate), kMixSeconds) != 0) {
        [self problem:@"there was no room to add the two sounds"];
        return;
    }
    _mixing = YES;
    _chunkSteps = MAX(1, lround(_chunk * _mix.rate / _mix.step));
    OADiag(@"recognition is given %@, where it names %@; a request is given %ld steps of %.0f ms", OAFormatName(_format), OAFormatName(native), _chunkSteps,
           1000.0 * _mix.step / _mix.rate);
    OASound *call = [[OASound alloc] init], *mic = [[OASound alloc] init];
    call.name = @"call";
    mic.name = @"mic";
    call->peak = mic->peak = -1;
    _sounds = @[ call, mic ];
    recognizer.queue = _results;
    _recognizer = recognizer;
    _ready = YES;
    say(ONAIR_LIVE_ALLOWED, @"");
    atomic_store(&listening, true);
}

// Hear takes a buffer of one of the two sounds, brings it to recognition's
// format, places it on the mix's timeline by its stamp, and hands on the
// steps of the mix it completes.
- (void)hear:(AVAudioPCMBuffer *)buffer sound:(int)sound stamp:(CMTime)stamp at:(double)wall {
    if (!_ready || _stopped || sound < 0 || sound >= (int)_sounds.count) return;
    OASound *s = _sounds[sound];
    s->count[OA_BUFFERS]++;
    s->count[OA_FRAMES_IN] += buffer.frameLength;
    s->peak = fmax(s->peak, OAPeak(buffer));
    if (!_started) {
        _origin = stamp;
        _originWall = wall - buffer.frameLength / buffer.format.sampleRate;
        _placedWall = OANow();
        _started = YES;
    }
    AVAudioPCMBuffer *converted = [self convert:buffer for:s];
    if (!converted.frameLength) {
        s->count[OA_EMPTY]++;
        return;
    }
    s->count[OA_CONVERTED] += converted.frameLength;
    long at = lround(CMTimeGetSeconds(CMTimeSubtract(stamp, _origin)) * _mix.rate);
    oa_speechmix_place(&_mix, sound, converted.floatChannelData[0], converted.frameLength, at);
    _placedWall = OANow();
    [self mixTo:oa_speechmix_ready(&_mix, -1)];
}

// Convert brings a buffer to the format recognition is given: one channel,
// at its rate. A converter is kept for each sound while its format holds.
- (AVAudioPCMBuffer *)convert:(AVAudioPCMBuffer *)buffer for:(OASound *)sound {
    if (!sound.converter || ![sound.from isEqual:buffer.format]) {
        sound.from = buffer.format;
        sound.converter = [[AVAudioConverter alloc] initFromFormat:buffer.format toFormat:_format];
        sound.converter.downmix = YES;
        if (++sound->count[OA_CONVERTERS] <= 3) {
            OADiag(@"%@ converter %ld: from %@ to %@%@", sound.name, sound->count[OA_CONVERTERS], OAFormatName(buffer.format), OAFormatName(_format),
                   sound.converter ? @"" : @", which couldn't be made");
        }
        if (!sound.converter) {
            [self problem:[NSString stringWithFormat:@"the %@ sound, %@, can't be converted for recognition", sound.name, buffer.format]];
            return nil;
        }
    }
    AVAudioFrameCount room = (AVAudioFrameCount)ceil(buffer.frameLength * _format.sampleRate / buffer.format.sampleRate) + 64;
    AVAudioPCMBuffer *out = [[AVAudioPCMBuffer alloc] initWithPCMFormat:_format frameCapacity:room];
    if (!out) return nil;
    __block BOOL given = NO;
    NSError *error = nil;
    AVAudioConverterOutputStatus status = [sound.converter convertToBuffer:out error:&error withInputFromBlock:^AVAudioBuffer *(AVAudioPacketCount count, AVAudioConverterInputStatus *inputStatus) {
        if (given) {
            *inputStatus = AVAudioConverterInputStatus_NoDataNow;
            return nil;
        }
        given = YES;
        *inputStatus = AVAudioConverterInputStatus_HaveData;
        return buffer;
    }];
    if (status == AVAudioConverterOutputStatus_Error) {
        [self problem:[NSString stringWithFormat:@"the %@ sound couldn't be converted: %@", sound.name, error.localizedDescription ?: @"no reason given"]];
        sound.converter = nil;
        return nil;
    }
    return out;
}

// MixTo hands on the steps of the mix up to ready, keeps each sound's
// loudness in them, and gives them to recognition.
- (void)mixTo:(long)ready {
    while (_mix.emitted + _mix.step <= ready) {
        AVAudioPCMBuffer *step = [[AVAudioPCMBuffer alloc] initWithPCMFormat:_format frameCapacity:(AVAudioFrameCount)_mix.step];
        long n = _mix.emitted / _mix.step;
        float loud[2];
        if (!step || !oa_speechmix_take(&_mix, ready, step.floatChannelData[0], loud)) break;
        step.frameLength = (AVAudioFrameCount)_mix.step;
        long slot = n % kHistory;
        _loudStep[slot] = n;
        _loud[slot][0] = loud[0];
        _loud[slot][1] = loud[1];
        _count[OA_STEPS]++;
        _peak = fmax(_peak, OAPeak(step));
        // Steps are held in order until recognition takes them, the most
        // recent kHeld of them.
        if (!_held.count) _heldFirst = n;
        [_held addObject:step];
        while (_held.count > kHeld) {
            [_held removeObjectAtIndex:0];
            _heldFirst++;
            _count[OA_DROPPED]++;
        }
        _heldMost = MAX(_heldMost, (long)_held.count);
    }
    [self resume];
}

// Resume gives recognition the steps held, in order: to the open request,
// or to a new one. None is given while a request is finishing, so that its
// final words come before the next begins, nor for a second after a
// request ended before its sound did.
- (void)resume {
    while (_held.count && !_stopped) {
        if (_current.ended > 0) return;
        if (!_current) {
            if (OANow() < _notBefore) return;
            [self begin];
        }
        AVAudioPCMBuffer *step = _held.firstObject;
        [_held removeObjectAtIndex:0];
        _heldFirst++;
        [_current.request appendAudioPCMBuffer:step];
        _current.steps++;
        if (_current.steps >= _chunkSteps) [self end];
    }
}

// Begin makes a request, on the Mac only, whose sound starts with the
// first step held.
- (void)begin {
    SFSpeechAudioBufferRecognitionRequest *request = [[SFSpeechAudioBufferRecognitionRequest alloc] init];
    request.requiresOnDeviceRecognition = YES;
    // Partial results come a few tenths of a second after their sound, and
    // Go takes each sentence of them once it is finished, so a question
    // is asked about a second after it is said rather than when its
    // request ends.
    request.shouldReportPartialResults = YES;
    // End marks, such as a question mark, help the live side tell where a
    // sentence ends and whether it asks something.
    request.addsPunctuation = YES;
    OARequest *r = [[OARequest alloc] init];
    r.number = ++_count[OA_BEGUN];
    r.request = request;
    r.start = _heldFirst;
    r.begun = OANow();
    double step = (double)_mix.step / _mix.rate;
    OADiag(@"request %ld begins at %.1f s of the mix, with %.1f s of it held, %@", r.number, r.start * step, _held.count * step,
           _settled > 0 ? [NSString stringWithFormat:@"%.0f ms after the last request ended", (r.begun - _settled) * 1000] : @"the first");
    r.task = [_recognizer recognitionTaskWithRequest:request resultHandler:^(SFSpeechRecognitionResult *result, NSError *error) {
        // What the live queue needs is taken here, on recognition's queue.
        BOOL got = result != nil, final = result.isFinal;
        NSArray<SFTranscriptionSegment *> *segments = got ? [result.bestTranscription.segments copy] : nil;
        NSString *text = got ? [result.bestTranscription.formattedString copy] : nil;
        long words = OAWords(text);
        double at = OANow();
        dispatch_async(OALiveQueue(), ^{
            @try {
                [self request:r result:got final:final words:words text:text segments:segments error:error at:at];
            } @catch (NSException *exception) {
                OAFail(self, exception);
            }
        });
    }];
    _current = r;
}

// End ends the open request's sound, so its final words come.
- (void)end {
    if (!_current || _current.ended > 0) return;
    [_current.request endAudio];
    _current.ended = OANow();
    _count[OA_ENDED]++;
}

// Request takes what a request's result handler was given, on the live
// queue: final words go to Go, and the first final or error ends the
// request, whereupon the next may begin. It runs after listening stops as
// well, so the log shows how requests end when they are called off.
- (void)request:(OARequest *)r
         result:(BOOL)got
          final:(BOOL)final
          words:(long)words
           text:(NSString *)text
       segments:(NSArray<SFTranscriptionSegment *> *)segments
          error:(NSError *)error
             at:(double)at {
    _count[OA_CALLS]++;
    if (got && !final) {
        _count[OA_PARTIALS]++;
        r.partials++;
        // A partial result goes to Go when its words changed, or the sound
        // went on; Go decides when a sentence of it is finished.
        BOOL changed = ![text ?: @"" isEqualToString:r.text ?: @""];
        if (r == _current && !r.final && !r.done && !_stopped && (changed || r.steps >= r.handed + kUpdateSteps)) {
            r.text = text;
            r.segments = segments;
            [self hand:r segments:segments partial:YES];
        }
    }
    NSString *when = [NSString stringWithFormat:@"%.0f ms after it began, %@%@", (at - r.begun) * 1000,
                                                r.ended > 0 ? [NSString stringWithFormat:@"%.0f ms after its sound ended", (at - r.ended) * 1000] : @"before its sound ended",
                                                _stopped ? @", after listening stopped" : @""];
    if (final) {
        _count[OA_FINALS]++;
        _count[OA_WORDS] += words;
        if (!words) _count[OA_EMPTY_FINALS]++;
        BOOL handed = !r.final && !_stopped && [self hand:r segments:segments partial:NO];
        if (handed) _count[OA_HANDED]++;
        r.final = YES;
        long untimed = 0;
        for (SFTranscriptionSegment *segment in segments) {
            if (segment.duration <= 0) untimed++;
        }
        OADiag(@"request %ld: final, %ld words in %lu segments (%ld with no length) over %ld steps, %@ to Go, after %ld partial results and %ld handed to Go with updates, %@", r.number, words,
               (unsigned long)segments.count, untimed, r.steps, handed ? @"handed" : @"not handed", r.partials, r.updates, when);
    }
    if (error) {
        _count[OA_ERRORS]++;
        BOOL normal = OAEnding(error) && (r.final || r.ended > 0 || r.calledOff > 0 || _stopped);
        OADiag(@"request %ld: error %@ %ld, %@, %@", r.number, error.domain, (long)error.code, normal ? @"a normal end, not shown" : @"shown", when);
        if (!normal) {
            _count[OA_SHOWN]++;
            [self problem:[NSString stringWithFormat:@"%@%@ (%@ %ld)", r.ended > 0 ? @"" : @"a request stopped before its sound ended: ",
                                                     error.localizedDescription ?: @"no reason given", error.domain, (long)error.code]];
        }
    }
    if (!got && !error) OADiag(@"request %ld: neither a result nor an error, %@", r.number, when);
    if ((!final && !error) || r.done) return;
    r.done = YES;
    if (r != _current) return;
    if (r.ended <= 0) {
        // The steps it was given are lost, and the next request waits a
        // moment.
        _count[OA_EARLY]++;
        _notBefore = r.begun + 1;
    }
    _current = nil;
    _settled = at;
    [self resume];
}

// Hand gives Go a request's words, final or partial: each segment's words
// and when it was said, from the start of the request's sound, when that
// sound began on the clock, and each sound's loudness through all the
// sound the request has been given, a step at a time. A partial result,
// or an update of how far the sound has gone, may have no words; a final
// one with none goes only when partial ones went.
- (BOOL)hand:(OARequest *)r segments:(NSArray<SFTranscriptionSegment *> *)segments partial:(BOOL)partial {
    long n = (long)segments.count, steps = r.steps;
    if (!n && !partial && !r.updates) return NO;
    onair_segment *list = calloc((size_t)n, sizeof *list);
    if (!list && !n) list = calloc(1, sizeof *list);
    double *mic = calloc((size_t)MAX(steps, 1), sizeof(double)), *call = calloc((size_t)MAX(steps, 1), sizeof(double));
    BOOL handed = list && mic && call;
    if (handed) {
        for (long i = 0; i < n; i++) {
            SFTranscriptionSegment *segment = segments[i];
            list[i] = (onair_segment){.text = segment.substring.UTF8String ?: "", .at = segment.timestamp, .length = segment.duration};
        }
        for (long i = 0; i < steps; i++) {
            long step = r.start + i, slot = step % kHistory;
            if (_loudStep[slot] != step) continue;
            call[i] = _loud[slot][0];
            mic[i] = _loud[slot][1];
        }
        double step = (double)_mix.step / _mix.rate;
        onairHeard(_originWall + r.start * step, list, (int)n, step, mic, call, (int)steps, partial ? 1 : 0, r.number);
        r.handed = steps;
        if (partial) {
            r.updates++;
            _count[OA_UPDATES]++;
        }
    }
    free(list);
    free(mic);
    free(call);
    return handed;
}

// Report logs what each sound and the mix have counted since listening
// started, and how loud each was since the last report.
- (void)report {
    if (!_sounds) {
        OADiag(@"not listening yet: capture offered %ld buffers of the call's sound and %ld of the microphone's", atomic_load(&offered[OA_SOUND_CALL]), atomic_load(&offered[OA_SOUND_MICROPHONE]));
        return;
    }
    for (int i = 0; i < 2; i++) {
        OASound *s = _sounds[i];
        long *c = s->count;
        OADiag(@"%@: offered %ld, not copied %ld, in %ld (%ld frames, peak %@), converters %ld, converted %ld frames (%ld conversions empty)", s.name, atomic_load(&offered[i]),
               atomic_load(&uncopied[i]), c[OA_BUFFERS], c[OA_FRAMES_IN], OADecibels(s->peak), c[OA_CONVERTERS], c[OA_CONVERTED], c[OA_EMPTY]);
        s->peak = -1;
    }
    long *c = _count;
    OADiag(@"mix: %ld steps (peak %@), %ld frames late and %ld early, %ld steps held now, %ld at most, %ld dropped; requests begun %ld, ended %ld, ended early %ld, "
           @"called off %ld; handler calls %ld, not final %ld (%ld handed to Go with updates), finals %ld (%ld empty, %ld handed to Go), words %ld, errors %ld (%ld shown)",
           c[OA_STEPS], OADecibels(_peak), _mix.late, _mix.early, (long)_held.count, _heldMost, c[OA_DROPPED], c[OA_BEGUN], c[OA_ENDED], c[OA_EARLY], c[OA_CALLED_OFF],
           c[OA_CALLS], c[OA_PARTIALS], c[OA_UPDATES], c[OA_FINALS], c[OA_EMPTY_FINALS], c[OA_HANDED], c[OA_WORDS], c[OA_ERRORS], c[OA_SHOWN]);
    _peak = -1;
}

// Tick, every quarter of a second, calls off a request whose final hasn't
// come, keeps the mix moving with the clock when no sound comes, so a
// request ends even then, tells Go how far the open request's sound has
// gone since its last partial result, and says when sound was dropped.
- (void)tick {
    if (_stopped) return;
    if (++_ticks % lround(5 / kTick) == 0) [self report];
    if (!_ready) return;
    double now = OANow();
    if (_current.ended > 0 && _current.calledOff <= 0 && now - _current.ended >= kFinalWait) {
        OADiag(@"request %ld: no final %.0f s after its sound ended, so it is called off", _current.number, kFinalWait);
        _current.calledOff = now;
        _count[OA_CALLED_OFF]++;
        [_current.task cancel];
    } else if (_current.calledOff > 0 && now - _current.calledOff >= kGoneWait) {
        OADiag(@"request %ld: still there %.0f s after it was called off, so the next begins", _current.number, kGoneWait);
        _current = nil;
        _settled = now;
    }
    if (_started) {
        long newest = MAX(_mix.heard[0] ? _mix.next[0] : 0, _mix.heard[1] ? _mix.next[1] : 0);
        [self mixTo:oa_speechmix_ready(&_mix, newest + lround((now - _placedWall) * _mix.rate))];
    } else {
        [self resume];
    }
    OARequest *r = _current;
    if (r && !r.final && r.ended <= 0 && r.steps >= r.handed + kUpdateSteps) [self hand:r segments:r.segments partial:YES];
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
    if (!_stopped) {
        [self report];
        OADiag(@"listening stops");
    }
    _stopped = YES;
    if (_current) {
        _current.calledOff = OANow();
        [_current.task cancel];
    }
    _current = nil;
    [_held removeAllObjects];
}

@end

void OALiveHear(CMSampleBufferRef samples, int sound) {
    if (!atomic_load(&listening) || (sound != OA_SOUND_CALL && sound != OA_SOUND_MICROPHONE)) return;
    atomic_fetch_add(&offered[sound], 1);
    if (atomic_fetch_add(&waiting, 1) >= mostWaiting) {
        atomic_fetch_sub(&waiting, 1);
        atomic_fetch_add(&dropped, 1);
        return;
    }
    CMTime stamp = CMSampleBufferGetPresentationTimeStamp(samples);
    AVAudioPCMBuffer *copy = nil;
    @try {
        copy = CMTIME_IS_NUMERIC(stamp) ? OACopySound(samples) : nil;
    } @catch (NSException *exception) {
        copy = nil;
    }
    if (!copy) {
        atomic_fetch_add(&uncopied[sound], 1);
        atomic_fetch_sub(&waiting, 1);
        return;
    }
    double wall = OANow();
    long mine = atomic_load(&generation);
    dispatch_async(OALiveQueue(), ^{
        atomic_fetch_sub(&waiting, 1);
        if (mine != atomic_load(&generation)) return;
        @try {
            [listener hear:copy sound:sound stamp:stamp at:wall];
        } @catch (NSException *exception) {
            OAFail(listener, exception);
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
        for (int s = 0; s < 2; s++) {
            atomic_store(&offered[s], 0);
            atomic_store(&uncopied[s], 0);
        }
        @try {
            listener = [[OAListener alloc] initWithChunk:chunk];
            [listener start];
            ticker = dispatch_source_create(DISPATCH_SOURCE_TYPE_TIMER, 0, 0, OALiveQueue());
            dispatch_source_set_timer(ticker, dispatch_time(DISPATCH_TIME_NOW, (int64_t)(kTick * NSEC_PER_SEC)), (uint64_t)(kTick * NSEC_PER_SEC), NSEC_PER_SEC / 40);
            dispatch_source_set_event_handler(ticker, ^{
                @try {
                    [listener tick];
                } @catch (NSException *exception) {
                    OAFail(listener, exception);
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

void onair_live_show(int visible, const char *display, const char *headline, const char *status, const char *calls, const onair_answer *answers, int answerCount,
                     const onair_link *links, int count) {
    NSMutableArray<NSDictionary<NSString *, NSString *> *> *rows = [NSMutableArray array];
    for (int i = 0; i < answerCount; i++) {
        [rows addObject:@{
            @"kind" : [NSString stringWithUTF8String:answers[i].kind ?: ""] ?: @"",
            @"text" : [NSString stringWithUTF8String:answers[i].text ?: ""] ?: @"",
            @"where" : [NSString stringWithUTF8String:answers[i].where ?: ""] ?: @"",
            @"link" : [NSString stringWithUTF8String:answers[i].link ?: ""] ?: @"",
        }];
    }
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
    NSString *c = [NSString stringWithUTF8String:calls ?: ""] ?: @"", *h = [NSString stringWithUTF8String:headline ?: ""] ?: @"";
    NSArray<NSString *> *said = c.length ? [c componentsSeparatedByString:@"\n"] : @[];
    long asked = atomic_fetch_add(&panelsAsked, 1) + 1;
    dispatch_async(dispatch_get_main_queue(), ^{
        if (asked <= panelsDone) return;
        panelsDone = asked;
        @try {
            OAPanelShow(visible != 0, d, h, s, said, rows, list);
        } @catch (NSException *exception) {
        }
    });
}
