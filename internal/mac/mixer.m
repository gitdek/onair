#import "mixer.h"

// The timeline counts frames at 48 kHz from the start. The ring holds the
// part of it not yet handed on.
enum {
    kRate = 48000,
    kRing = kRate * 4,     // four seconds
    kChunk = 4096,         // frames handed on at a time
    kPatience = kRate / 2, // how long a quiet source is waited for
    kSlip = kRate / 50,    // how far a stamp may stray before it moves its source
};

typedef struct {
    BOOL heard; // a buffer has come from it
    BOOL held;  // its converter may hold the end of what it was given
    long next;  // where its next frame goes on the timeline
} OASource;

// Two voices at once can pass full scale. Past 0.8 the sum is bent smoothly
// toward 1, where cutting it off would crackle.
static float limit(float x) {
    if (!isfinite(x)) return 0;
    float a = fabsf(x);
    if (a <= 0.8f) return x;
    float y = 0.8f + 0.2f * tanhf((a - 0.8f) / 0.2f);
    return x < 0 ? -y : y;
}

@implementation OAMixer {
    void (^_sink)(CMSampleBufferRef);
    int _sources;
    AVAudioFormat *_out;
    AVAudioConverter *_converter[2];
    OASource _source[2];
    float *_ring;   // kRing frames of two channels, frame _emitted at _head
    long _head;
    long _emitted;  // frames handed on
    CMTime _origin; // frame 0, on the stamps' clock
    BOOL _started;
}

- (instancetype)initWithSources:(int)sources sink:(void (^)(CMSampleBufferRef))sink {
    if (!(self = [super init]) || sources < 1 || sources > 2) return nil;
    _sources = sources;
    _sink = [sink copy];
    _out = [[AVAudioFormat alloc] initWithCommonFormat:AVAudioPCMFormatFloat32 sampleRate:kRate channels:2 interleaved:YES];
    _ring = calloc(kRing * 2, sizeof(float));
    if (!_out || !_ring) return nil;
    return self;
}

- (void)dealloc {
    free(_ring);
}

- (CMAudioFormatDescriptionRef)format {
    return _out.formatDescription;
}

- (void)startAt:(CMTime)origin {
    _origin = origin;
    _started = CMTIME_IS_NUMERIC(origin);
}

// Where a moment on the stamps' clock falls on the timeline.
- (long)frameAt:(CMTime)time {
    return (long)CMTimeConvertScale(CMTimeSubtract(time, _origin), kRate, kCMTimeRoundingMethod_RoundHalfAwayFromZero).value;
}

- (void)add:(CMSampleBufferRef)samples source:(int)source {
    if (!_started || source < 0 || source >= _sources || !CMSampleBufferIsValid(samples)) return;
    CMTime stamp = CMSampleBufferGetPresentationTimeStamp(samples);
    CMAudioFormatDescriptionRef description = CMSampleBufferGetFormatDescription(samples);
    AVAudioFrameCount frames = (AVAudioFrameCount)CMSampleBufferGetNumSamples(samples);
    if (!CMTIME_IS_NUMERIC(stamp) || !description || frames == 0) return;
    AVAudioFormat *format = [[AVAudioFormat alloc] initWithCMAudioFormatDescription:description];
    if (!format || format.sampleRate <= 0) return;
    OASource *s = &_source[source];
    long at = [self frameAt:stamp];
    // A source's buffers follow one another, whatever their stamps say, so
    // its sound has no seams. A stamp places only the first buffer, and one
    // that strays from where the last ended, as after a quiet spell. What a
    // converter still holds belongs before a stray, and before a new format.
    BOOL moved = !s->heard || labs(at - s->next) > kSlip;
    AVAudioConverter *converter = _converter[source];
    if (converter && (moved || ![converter.inputFormat isEqual:format])) {
        [self drain:source];
        if (![converter.inputFormat isEqual:format]) converter = nil;
    }
    if (!converter) converter = [[AVAudioConverter alloc] initFromFormat:format toFormat:_out];
    // A microphone mostly has one channel. It belongs in both ears.
    if (converter && format.channelCount == 1 && converter != _converter[source]) converter.channelMap = @[ @0, @0 ];
    _converter[source] = converter;
    if (moved) s->next = at;
    s->heard = YES;
    AVAudioPCMBuffer *input = converter ? [[AVAudioPCMBuffer alloc] initWithPCMFormat:format frameCapacity:frames] : nil;
    if (!input) return;
    input.frameLength = frames;
    if (CMSampleBufferCopyPCMDataIntoAudioBufferList(samples, 0, (int32_t)frames, input.mutableAudioBufferList) != noErr) {
        // Its next buffer is placed by its stamp, not after this one.
        s->heard = NO;
        return;
    }
    [self convert:input source:source];
    [self emitReady];
}

// Convert brings a buffer to the timeline's format and places it. The
// converter runs until it asks for more, so all it keeps of the buffer is
// the tail it needs the next one to finish.
- (void)convert:(AVAudioPCMBuffer *)input source:(int)source {
    AVAudioConverter *converter = _converter[source];
    AVAudioFrameCount capacity = (AVAudioFrameCount)ceil(input.frameLength * (double)kRate / input.format.sampleRate) + 256;
    __block BOOL given = NO;
    for (;;) {
        AVAudioPCMBuffer *output = [[AVAudioPCMBuffer alloc] initWithPCMFormat:_out frameCapacity:capacity];
        if (!output) return;
        NSError *error = nil;
        AVAudioConverterOutputStatus status = [converter convertToBuffer:output error:&error withInputFromBlock:^AVAudioBuffer *(AVAudioPacketCount count, AVAudioConverterInputStatus *state) {
            if (given) {
                *state = AVAudioConverterInputStatus_NoDataNow;
                return nil;
            }
            given = YES;
            *state = AVAudioConverterInputStatus_HaveData;
            return input;
        }];
        if (status == AVAudioConverterOutputStatus_Error) {
            // Start the source afresh, placed by the stamp of its next buffer.
            _converter[source] = nil;
            _source[source].heard = NO;
            return;
        }
        if (output.frameLength > 0) [self place:output source:source];
        _source[source].held = YES;
        if (status != AVAudioConverterOutputStatus_HaveData) return;
    }
}

// Drain places what a converter still holds after what it gave, and starts
// it afresh.
- (void)drain:(int)source {
    AVAudioConverter *converter = _converter[source];
    if (!converter || !_source[source].held) return;
    _source[source].held = NO;
    for (;;) {
        AVAudioPCMBuffer *output = [[AVAudioPCMBuffer alloc] initWithPCMFormat:_out frameCapacity:kChunk];
        if (!output) break;
        NSError *error = nil;
        AVAudioConverterOutputStatus status = [converter convertToBuffer:output error:&error withInputFromBlock:^AVAudioBuffer *(AVAudioPacketCount count, AVAudioConverterInputStatus *state) {
            *state = AVAudioConverterInputStatus_EndOfStream;
            return nil;
        }];
        if (status != AVAudioConverterOutputStatus_Error && output.frameLength > 0) [self place:output source:source];
        if (status != AVAudioConverterOutputStatus_HaveData) break;
    }
    [converter reset];
}

// Place adds converted frames to the ring, where their source has reached.
- (void)place:(AVAudioPCMBuffer *)buffer source:(int)source {
    OASource *s = &_source[source];
    const float *in = (const float *)buffer.audioBufferList->mBuffers[0].mData;
    long first = s->next, frames = buffer.frameLength;
    s->next += frames;
    for (long i = 0; i < frames; i++) {
        long frame = first + i;
        if (frame < 0) continue; // from before the start
        if (frame < _emitted) {
            _late++;
            continue;
        }
        if (frame >= _emitted + kRing) {
            _early++;
            continue;
        }
        long p = ((_head + (frame - _emitted)) % kRing) * 2;
        _ring[p] += in[2 * i];
        _ring[p + 1] += in[2 * i + 1];
    }
}

// Hand on what every source has reached. One that hasn't been heard yet, or
// has gone quiet, is waited for a moment at most.
- (void)emitReady {
    long ready = LONG_MAX, newest = LONG_MIN;
    for (int i = 0; i < _sources; i++) {
        long next = _source[i].heard ? _source[i].next : 0;
        ready = MIN(ready, next);
        newest = MAX(newest, next);
    }
    [self passTo:MAX(ready, newest - kPatience)];
}

- (void)advanceTo:(CMTime)time {
    if (!_started || !CMTIME_IS_NUMERIC(time)) return;
    [self passTo:[self frameAt:time] - kPatience];
}

// PassTo hands on the timeline up to end, in whole chunks so the encoder
// gets steady buffers. A source it leaves behind first gives up what its
// converter holds, while that still has a place to go.
- (void)passTo:(long)end {
    if (end <= _emitted) return;
    end -= (end - _emitted) % kChunk;
    for (int i = 0; i < _sources; i++) {
        if (_source[i].heard && _source[i].next < end) [self drain:i];
    }
    [self emitTo:end];
}

- (void)flush {
    if (!_started) return;
    long end = _emitted;
    for (int i = 0; i < _sources; i++) {
        if (!_source[i].heard) continue;
        [self drain:i];
        end = MAX(end, _source[i].next);
    }
    [self emitTo:MIN(end, _emitted + kRing)];
}

// EmitTo hands on the frames up to end, in chunks, and clears them.
- (void)emitTo:(long)end {
    while (_emitted < end) {
        long frames = MIN(end - _emitted, (long)kChunk);
        size_t bytes = (size_t)frames * 2 * sizeof(float);
        CMBlockBufferRef block = NULL;
        CMSampleBufferRef mixed = NULL;
        char *data = NULL;
        if (CMBlockBufferCreateWithMemoryBlock(kCFAllocatorDefault, NULL, bytes, kCFAllocatorDefault, NULL, 0, bytes, kCMBlockBufferAssureMemoryNowFlag, &block) == noErr &&
            CMBlockBufferGetDataPointer(block, 0, NULL, NULL, &data) == noErr) {
            float *out = (float *)data;
            for (long i = 0; i < frames; i++) {
                long p = ((_head + i) % kRing) * 2;
                out[2 * i] = limit(_ring[p]);
                out[2 * i + 1] = limit(_ring[p + 1]);
            }
            // On the recording's timeline, from zero, each buffer starting
            // where the last one ended.
            CMAudioSampleBufferCreateReadyWithPacketDescriptions(kCFAllocatorDefault, block, _out.formatDescription, frames, CMTimeMake(_emitted, kRate), NULL, &mixed);
        }
        if (block) CFRelease(block);
        for (long i = 0; i < frames; i++) {
            long p = ((_head + i) % kRing) * 2;
            _ring[p] = 0;
            _ring[p + 1] = 0;
        }
        _head = (_head + frames) % kRing;
        _emitted += frames;
        // A buffer that couldn't be made is left out, and the timeline goes on.
        if (!mixed) continue;
        _handed++;
        _sink(mixed);
        CFRelease(mixed);
    }
}

@end
