// OAMixer makes one sound track of two: the display's sound and the
// microphone. Each arrives in its own format, at its own pace, stamped by
// the same clock. The mixer puts both on one timeline at 48 kHz in stereo,
// adds them, and hands on what both have reached, without waiting long for
// one that has gone quiet. It is used from one queue at a time.
#import <AVFoundation/AVFoundation.h>

@interface OAMixer : NSObject
// The format of what the mixer hands on.
@property(readonly) CMAudioFormatDescriptionRef format;
// Frames left out: late ones came after their place was handed on, early
// ones were stamped too far ahead of it to be held.
@property(readonly) long late;
@property(readonly) long early;
// Buffers handed on.
@property(readonly) long handed;
// Sources is how many will be added: 1 for the display's sound alone, 2 with
// the microphone. The sink gets each mixed buffer, in order.
- (instancetype)initWithSources:(int)sources sink:(void (^)(CMSampleBufferRef samples))sink;
// Start sets where the timeline begins: origin, on the clock the sound is
// stamped with, is its first frame, which the mixer hands on at time zero.
// Sound from before it is left out.
- (void)startAt:(CMTime)origin;
// Add mixes in a buffer from source 0, the display, or 1, the microphone.
- (void)add:(CMSampleBufferRef)samples source:(int)source;
// Advance hands on the timeline up to a moment before time, on the stamps'
// clock, silent where no source reached, so it keeps pace with the picture
// when no sound comes. Capture calls it twice a second.
- (void)advanceTo:(CMTime)time;
// Flush hands on everything mixed so far, with what the converters held.
- (void)flush;
@end
