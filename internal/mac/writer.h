// OAWriter cuts picture and sound into fragmented MPEG-4 segments and hands
// each one to Go as soon as it is whole.
#import <AVFoundation/AVFoundation.h>

@interface OAWriter : NSObject <AVAssetWriterDelegate>
@property(readonly) BOOL started;
@property(readonly) long droppedFrames;
@property(readonly) long droppedSound;
// Why the writer stopped, when it did.
@property(readonly) NSError *error;
- (instancetype)initWithWidth:(int)width height:(int)height fps:(int)fps pieceSeconds:(double)seconds bitrate:(int)bitrate realTime:(BOOL)realTime;
// Tracks are added before the writer starts. A sound track's settings follow
// the format its samples arrive in.
- (int)addSoundTrack:(CMFormatDescriptionRef)format;
- (BOOL)startAt:(CMTime)time error:(NSError **)error;
- (BOOL)appendPicture:(CVPixelBufferRef)pixels at:(CMTime)time;
- (BOOL)appendSound:(CMSampleBufferRef)samples track:(int)track;
- (void)finish:(void (^)(NSError *error))done;
@end
