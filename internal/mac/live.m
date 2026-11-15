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
    OA_BUFFERS,      // buffers that reached the listener
    OA_FRAMES_IN,    // their frames
    OA_CONVERTERS,   // converters made
    OA_CONVERTED,    // frames that came out of conversion
    OA_EMPTY,        // conversions that gave none
    OA_APPENDED,     // frames appended to a request
    OA_BEGUN,        // requests begun
    OA_ENDED,        // requests whose sound was ended
    OA_CALLS,        // calls of a request's result handler
    OA_PARTIALS,     // results that weren't final
    OA_FINALS,       // final results
    OA_EMPTY_FINALS, // final results with no words
    OA_WORDS,        // words in final results
    OA_HANDED,       // final results handed to Go
    OA_ERRORS,       // errors, hidden ones included
    OA_COUNTS,
};

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
@class OATrace;

@interface OAVoice : NSObject {
  @public
    long count[OA_COUNTS];   // for the diagnostic log
    double peakIn, peakOut;  // the loudest sample into and out of conversion since the last count, or -1
}
@property int speaker;
@property int sound;             // OA_SOUND_CALL or OA_SOUND_MICROPHONE
@property(copy) NSString *name;  // as the diagnostic log names it
@property(strong) OATrace *trace; // the open request's
@property(strong) SFSpeechRecognizer *recognizer;
@property(strong) SFSpeechAudioBufferRecognitionRequest *request;
@property(strong) SFSpeechRecognitionTask *task;
@property(strong) AVAudioConverter *converter;
@property(strong) AVAudioFormat *from;
@property double opened; // when the request was made, on the clock
@end

@implementation OAVoice
@end

// OATrace follows one request for the diagnostic log: its number, when it
// began and when its sound ended, on the clock, and the results it gave
// that weren't final. It is used on the live queue only.
@interface OATrace : NSObject
@property long number, partials, partialWords;
@property double begun, ended;
@end

@implementation OATrace
@end

// OAListener listens while a recording runs with Live Context on. It is
// used on the live queue only.
@interface OAListener : NSObject
- (instancetype)initWithChunk:(double)chunk voices:(int)voices;
- (void)start;
- (void)hear:(AVAudioPCMBuffer *)buffer sound:(int)sound at:(double)wall;
- (void)tick;
- (void)stop;
@end

@implementation OAListener {
    double _chunk;
    int _wanted; // the ONAIR_VOICE_* to recognize
    long _ticks;
    BOOL _ready, _stopped;
    NSArray<OAVoice *> *_voices; // the call's sound, then the microphone
    NSMutableArray<SFSpeechRecognitionTask *> *_ending; // ended requests, until their words come
    NSOperationQueue *_results;
    AVAudioFormat *_format; // what recognition is given
    long _droppedSaid;
    NSMutableSet<NSString *> *_said; // problems already reported
}

- (instancetype)initWithChunk:(double)chunk voices:(int)voices {
    if (!(self = [super init])) return nil;
    _chunk = chunk > 1 ? chunk : 15;
    _wanted = voices & (ONAIR_VOICE_CALL | ONAIR_VOICE_MIC) ?: ONAIR_VOICE_CALL | ONAIR_VOICE_MIC;
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
        OADiag(@"listening starts, but this copy can't ask macOS about speech recognition");
        say(ONAIR_LIVE_REFUSED, @"this copy of On Air isn't the app tools/build-app makes, so macOS can't be asked");
        return;
    }
    SFSpeechRecognizerAuthorizationStatus status = [SFSpeechRecognizer authorizationStatus];
    OADiag(@"listening starts: speech recognition %s, sounds %@, a request every %.0f s", OAAuthName(status),
           _wanted == ONAIR_VOICE_CALL ? @"the call's only" : (_wanted == ONAIR_VOICE_MIC ? @"the microphone's only" : @"both"), _chunk);
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
    BOOL callOn = (_wanted & ONAIR_VOICE_CALL) != 0, micOn = (_wanted & ONAIR_VOICE_MIC) != 0;
    SFSpeechRecognizer *first = [[SFSpeechRecognizer alloc] init];
    NSLocale *locale = first.locale ?: NSLocale.currentLocale;
    NSString *language = [NSLocale.currentLocale localizedStringForLocaleIdentifier:locale.localeIdentifier] ?: locale.localeIdentifier;
    OADiag(@"recognizer one: %@", OARecognizerName(first));
    // Without the Mac's own recognition for the language, nothing is
    // recognized: the call's words never go to Apple's servers.
    if (!first || !first.supportsOnDeviceRecognition) {
        say(ONAIR_LIVE_UNSUPPORTED, language);
        return;
    }
    // A sound left out gets no recognizer. The microphone's is always the
    // second, made for the first's language, so that recognizing it alone
    // changes nothing else.
    SFSpeechRecognizer *second = nil;
    if (micOn) {
        second = [[SFSpeechRecognizer alloc] initWithLocale:locale];
        OADiag(@"recognizer two: %@", OARecognizerName(second));
        if (!second || !second.supportsOnDeviceRecognition) {
            say(ONAIR_LIVE_UNSUPPORTED, language);
            return;
        }
    }
    AVAudioFormat *format = [[SFSpeechAudioBufferRecognitionRequest alloc] init].nativeAudioFormat;
    BOOL native = format && format.sampleRate > 0 && format.channelCount >= 1;
    if (!native) {
        format = [[AVAudioFormat alloc] initWithCommonFormat:AVAudioPCMFormatFloat32 sampleRate:16000 channels:1 interleaved:NO];
    }
    OADiag(@"recognition is given %@%@", OAFormatName(format), native ? @", its native format" : @", since it named no native format");
    _format = format;
    OAVoice *call = [[OAVoice alloc] init], *person = [[OAVoice alloc] init];
    call.speaker = ONAIR_SPEAKER_OTHERS;
    call.sound = OA_SOUND_CALL;
    call.name = @"call";
    call.recognizer = callOn ? first : nil;
    person.speaker = ONAIR_SPEAKER_PERSON;
    person.sound = OA_SOUND_MICROPHONE;
    person.name = @"mic";
    person.recognizer = second;
    for (OAVoice *voice in @[ call, person ]) {
        voice.recognizer.queue = _results;
        voice->peakIn = voice->peakOut = -1;
    }
    _voices = @[ call, person ];
    _ready = YES;
    say(ONAIR_LIVE_ALLOWED, @"");
    atomic_store(&listening, true);
}

- (void)hear:(AVAudioPCMBuffer *)buffer sound:(int)sound at:(double)wall {
    if (!_ready || _stopped || sound < 0 || sound >= (int)_voices.count) return;
    OAVoice *voice = _voices[sound];
    voice->count[OA_BUFFERS]++;
    voice->count[OA_FRAMES_IN] += buffer.frameLength;
    voice->peakIn = fmax(voice->peakIn, OAPeak(buffer));
    // A sound left out is counted, and goes no further.
    if (!voice.recognizer) return;
    if (voice.request && wall - voice.opened >= _chunk) [self end:voice];
    AVAudioPCMBuffer *converted = [self convert:buffer for:voice];
    if (!converted.frameLength) {
        voice->count[OA_EMPTY]++;
        return;
    }
    voice->count[OA_CONVERTED] += converted.frameLength;
    voice->peakOut = fmax(voice->peakOut, OAPeak(converted));
    if (!voice.request) [self begin:voice at:wall - buffer.frameLength / buffer.format.sampleRate];
    [voice.request appendAudioPCMBuffer:converted];
    voice->count[OA_APPENDED] += converted.frameLength;
}

// Convert brings a buffer to the format recognition is given: one channel,
// at its rate. A converter is kept for each sound while its format holds.
- (AVAudioPCMBuffer *)convert:(AVAudioPCMBuffer *)buffer for:(OAVoice *)voice {
    if (!voice.converter || ![voice.from isEqual:buffer.format]) {
        voice.from = buffer.format;
        voice.converter = [[AVAudioConverter alloc] initFromFormat:buffer.format toFormat:_format];
        voice.converter.downmix = YES;
        if (++voice->count[OA_CONVERTERS] <= 3) {
            OADiag(@"%@ converter %ld: from %@ to %@%@", voice.name, voice->count[OA_CONVERTERS], OAFormatName(buffer.format), OAFormatName(_format),
                   voice.converter ? @"" : @", which couldn't be made");
        }
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
    OATrace *trace = [[OATrace alloc] init];
    trace.number = ++voice->count[OA_BEGUN];
    trace.begun = voice.opened;
    voice.trace = trace;
    voice.task = [voice.recognizer recognitionTaskWithRequest:request resultHandler:^(SFSpeechRecognitionResult *result, NSError *error) {
        BOOL handed = NO;
        if (result.isFinal) {
            SFTranscription *best = result.bestTranscription;
            SFTranscriptionSegment *first = best.segments.firstObject, *last = best.segments.lastObject;
            if (best.formattedString.length && first && last) {
                onairHeard(speaker, (char *)best.formattedString.UTF8String, began + first.timestamp, began + last.timestamp + last.duration);
                handed = YES;
            }
        }
        // The diagnostic log hears of each call, in counts and codes, on
        // the live queue.
        BOOL got = result != nil, final = result.isFinal, quiet = error && OAQuietEnd(error);
        long words = OAWords(result.bestTranscription.formattedString);
        unsigned long segments = result.bestTranscription.segments.count;
        NSString *domain = error.domain;
        long code = error.code;
        double at = OANow();
        dispatch_async(OALiveQueue(), ^{
            [self traced:trace voice:voice at:at result:got final:final words:words segments:segments handed:handed domain:domain code:code quiet:quiet];
        });
        if (error && !OAQuietEnd(error)) {
            NSString *what = [NSString stringWithFormat:@"%@ (%@ %ld)", error.localizedDescription ?: @"no reason given", error.domain, (long)error.code];
            dispatch_async(OALiveQueue(), ^{
                [self problem:what];
            });
        }
    }];
}

// Traced counts what a request's result handler was given, and logs it. It
// runs after listening stops as well, so the log shows how requests end
// when they are called off.
- (void)traced:(OATrace *)trace voice:(OAVoice *)voice at:(double)at result:(BOOL)got final:(BOOL)final words:(long)words segments:(unsigned long)segments handed:(BOOL)handed domain:(NSString *)domain code:(long)code quiet:(BOOL)quiet {
    voice->count[OA_CALLS]++;
    if (got && !final) {
        voice->count[OA_PARTIALS]++;
        trace.partials++;
        trace.partialWords = words;
    }
    NSString *when = [NSString stringWithFormat:@"%.0f ms after it began, %@%@%@", (at - trace.begun) * 1000,
                                                trace.ended > 0 ? [NSString stringWithFormat:@"%.0f ms after its sound ended", (at - trace.ended) * 1000] : @"before its sound ended",
                                                trace.partials ? [NSString stringWithFormat:@", after %ld results not final, the last of %ld words", trace.partials, trace.partialWords] : @"",
                                                _stopped ? @", after listening stopped" : @""];
    if (final) {
        voice->count[OA_FINALS]++;
        voice->count[OA_WORDS] += words;
        if (!words) voice->count[OA_EMPTY_FINALS]++;
        if (handed) voice->count[OA_HANDED]++;
        OADiag(@"%@ request %ld: final, %ld words in %lu segments, %@ to Go, %@", voice.name, trace.number, words, segments, handed ? @"handed" : @"not handed", when);
    }
    if (domain) {
        voice->count[OA_ERRORS]++;
        OADiag(@"%@ request %ld: error %@ %ld, %@, %@", voice.name, trace.number, domain, code, quiet ? @"hidden" : @"shown", when);
    }
    if (!got && !domain) OADiag(@"%@ request %ld: neither a result nor an error, %@", voice.name, trace.number, when);
}

// Report logs what each sound has counted since listening started, and how
// loud it was since the last report.
- (void)report {
    if (!_voices) {
        OADiag(@"not listening yet: capture offered %ld buffers of the call's sound and %ld of the microphone's", atomic_load(&offered[OA_SOUND_CALL]), atomic_load(&offered[OA_SOUND_MICROPHONE]));
        return;
    }
    for (OAVoice *v in _voices) {
        long *c = v->count;
        OADiag(@"%@%@: offered %ld, not copied %ld, in %ld (%ld frames, peak %@), converters %ld, converted %ld frames (peak %@, %ld conversions empty), appended %ld, "
               @"requests begun %ld, ended %ld, handler calls %ld, not final %ld, finals %ld (%ld empty, %ld handed to Go), words %ld, errors %ld",
               v.name, v.recognizer ? @"" : @" (left out)", atomic_load(&offered[v.sound]), atomic_load(&uncopied[v.sound]), c[OA_BUFFERS], c[OA_FRAMES_IN], OADecibels(v->peakIn),
               c[OA_CONVERTERS], c[OA_CONVERTED], OADecibels(v->peakOut), c[OA_EMPTY], c[OA_APPENDED], c[OA_BEGUN], c[OA_ENDED], c[OA_CALLS], c[OA_PARTIALS],
               c[OA_FINALS], c[OA_EMPTY_FINALS], c[OA_HANDED], c[OA_WORDS], c[OA_ERRORS]);
        v->peakIn = v->peakOut = -1;
    }
}

// End ends a request's sound, so its finished words come. Its task is kept
// a while, until they do.
- (void)end:(OAVoice *)voice {
    if (voice.request) {
        voice->count[OA_ENDED]++;
        voice.trace.ended = OANow();
    }
    [voice.request endAudio];
    if (voice.task) [_ending addObject:voice.task];
    while (_ending.count > 8) [_ending removeObjectAtIndex:0];
    voice.request = nil;
    voice.task = nil;
    voice.trace = nil;
}

// Tick, every second, ends a request that has run its time even when no
// sound comes, and says when sound was dropped.
- (void)tick {
    if (_stopped) return;
    if (++_ticks % 5 == 0) [self report];
    if (!_ready) return;
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
    if (!_stopped) {
        [self report];
        OADiag(@"listening stops");
    }
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
    BOOL counted = sound == OA_SOUND_CALL || sound == OA_SOUND_MICROPHONE;
    if (counted) atomic_fetch_add(&offered[sound], 1);
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
        if (counted) atomic_fetch_add(&uncopied[sound], 1);
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

void onair_live_listen(int on, double chunk, int voices) {
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
            listener = [[OAListener alloc] initWithChunk:chunk voices:voices];
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

void onair_live_show(int visible, const char *display, const char *headline, const char *status, const char *calls, const onair_link *links, int count) {
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
            OAPanelShow(visible != 0, d, h, s, said, list);
        } @catch (NSException *exception) {
        }
    });
}
