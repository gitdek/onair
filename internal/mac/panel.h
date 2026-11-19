// The live panel: a panel at the right edge of a display that floats
// above other windows, never takes focus, and is kept out of screen
// sharing. It shows a quiet status line, what was said that differs from
// the record, and a few cards, each a title and short lines with their
// sources, a click on one opening it in the browser. It decides nothing:
// Go says what it shows.
#import <AppKit/AppKit.h>

// OAPanelShow shows the panel at the right edge of a display, named as
// OADisplayID names it, or of the main display when there is no such
// display, with a status line, what a model said about the call, a line
// each, and rows, each a dictionary of strings, "kind", "text", "where"
// and "link": a difference from the record, then each card's title, its
// lines, each followed by its sources, and a word on it. It hides the
// panel when visible is NO. Main thread only.
void OAPanelShow(BOOL visible, NSString *display, NSString *status, NSArray<NSString *> *calls, NSArray<NSDictionary<NSString *, NSString *> *> *rows);

// OAPanelContent lays out what the panel shows, at its width, without a
// window. OAPanelShow puts it in the panel.
NSView *OAPanelContent(NSString *status, NSArray<NSString *> *calls, NSArray<NSDictionary<NSString *, NSString *> *> *rows);
