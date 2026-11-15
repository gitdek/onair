// The live side's mix of capture's two sounds, for speech recognition. Only
// one recognition runs at a time in a process, so the call's sound and the
// microphone are recognized as one sound. Each, already in one channel at
// recognition's rate, is placed on one timeline as the recording's mixer
// places them: by its stamp when it is first heard, or strays, and after its
// last frames otherwise. The two are handed on added, a step of a tenth of a
// second at a time, with how loud each was in that step before they were
// added. It is plain C, used from one queue at a time.
#ifndef ONAIR_SPEECHMIX_H
#define ONAIR_SPEECHMIX_H

typedef struct {
    long rate;        // frames a second
    long step;        // frames a step
    long patience;    // how long a sound that has gone quiet is waited for
    long slip;        // how far a stamp may stray before it moves its sound
    long size;        // frames the ring holds of each sound, whole steps
    float *ring;      // each sound's frames not yet handed on, sound s's from ring + s * size
    long head;        // where frame `emitted` is in the ring
    long emitted;     // frames handed on, whole steps
    int heard[2];     // a buffer has come from the sound
    long next[2];     // where the sound's next frame goes on the timeline
    long late, early; // frames left out: behind what was handed on, or too far ahead to hold
} oa_speechmix;

// Init makes a mix at rate that holds seconds of each sound. It returns 0,
// or -1 when it can't.
int oa_speechmix_init(oa_speechmix *m, long rate, long seconds);
void oa_speechmix_free(oa_speechmix *m);
// Place puts count frames of sound 0, the call's, or 1, the microphone's,
// on the timeline, where its stamp, at, puts them if it moves.
void oa_speechmix_place(oa_speechmix *m, int sound, const float *frames, long count, long at);
// Ready is how far the mix may be handed on: as far as both sounds have
// reached, or the newest less patience, or now less patience, when now is
// a frame on the timeline and not negative. A sound not heard yet has
// reached the start.
long oa_speechmix_ready(const oa_speechmix *m, long now);
// Take hands on the next step, when ready reaches its end: the two sounds
// added, within full scale, as step frames in out, and each one's RMS in
// loud. It returns 1, or 0 when there is no step to take.
int oa_speechmix_take(oa_speechmix *m, long ready, float *out, float loud[2]);

#endif
