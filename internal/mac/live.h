// What capture gives the live side: a copy of each buffer of its two
// sounds, while the live side listens.
#import <CoreMedia/CoreMedia.h>

// The two sounds capture hands over.
enum {
    OA_SOUND_CALL = 0,       // the display's sound: everyone else on the call
    OA_SOUND_MICROPHONE = 1, // the microphone: the person recording
};

// OALiveHear copies a buffer of capture's sound for speech recognition,
// when the live side is listening, and returns at once. It is called on
// capture's queue, which it never holds up: a buffer that would make too
// many wait for recognition is dropped, and counted.
void OALiveHear(CMSampleBufferRef samples, int sound);
