// The part of On Air that touches macOS: the menu-bar item, the screen and
// sound capture, and the writer that cuts them into segments. It decides
// nothing. It reports what happened to Go through the three callbacks below,
// and Go tells it what to show.
#ifndef ONAIR_RECORDER_H
#define ONAIR_RECORDER_H

typedef struct {
    int rows;            // the picture's height at most; its width follows the display
    int fps;             // frames a second at most
    double pieceSeconds; // how long a piece is
    int videoBitrate;    // bits a second
    int microphone;      // 1 to record the microphone beside the display's sound
} onair_config;

// What onairEvent reports.
enum {
    ONAIR_STARTED = 1,  // capture is running
    ONAIR_FINISHED = 2, // the last segment was handed over
    ONAIR_FAILED = 3,   // capture stopped or never started; the message says why
};

// What onairMenu reports.
enum {
    ONAIR_MENU_START = 1,
    ONAIR_MENU_STOP = 2,
    ONAIR_MENU_LAST = 3,
    ONAIR_MENU_FOLDER = 4,
    ONAIR_MENU_QUIT = 5,
};

// Start asks for capture and returns at once. ONAIR_STARTED or ONAIR_FAILED
// follows. Segments arrive through onairPiece: kind 0 is the opening
// segment, kind 1 a media segment.
void onair_start(onair_config cfg);
// Stop ends capture. ONAIR_FINISHED follows the last segment.
void onair_stop(void);

// Selftest writes segments from made-up frames and sound through the same
// writer, with no capture, and returns once the last one was handed over.
// It returns 0, or -1 with a message the caller frees.
int onair_selftest(double seconds, int audioTracks, double pieceSeconds, char **error);

// Probe reads a movie file's length and tracks. It returns 0, or -1 with a
// message the caller frees.
int onair_probe(const char *path, double *seconds, int *videoTracks, int *audioTracks, char **error);

// Run shows the menu-bar item and runs the app. It doesn't return.
void onair_run(void);
// Show sets what the menu-bar item says. Any thread may call it.
void onair_show(int recording, const char *last, const char *note);
// Reveal shows a file or a folder in the Finder.
void onair_reveal(const char *path);
// Quit ends the app.
void onair_quit(void);

#endif
