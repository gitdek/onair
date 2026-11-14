// What the menu needs to pick what to record: the connected displays, an
// area of one, or a window picked with the system's picker.
#import <AppKit/AppKit.h>
#import <ScreenCaptureKit/ScreenCaptureKit.h>

// DisplayID names a display the same way across launches and reconnections.
NSString *OADisplayID(CGDirectDisplayID display);
// ReportDisplays tells Go which displays are connected. Main thread only.
void OAReportDisplays(void);
// PickedWindow is the filter for the picked window, and the window, while
// On Air holds one. Main thread only.
SCContentFilter *OAPickedWindow(CGWindowID *window);
// WindowOpen says whether a window is still there.
BOOL OAWindowOpen(CGWindowID window);
// ForgetClosedWindow lets go of a picked window that has closed, and says
// whether it did. Main thread only.
BOOL OAForgetClosedWindow(void);
