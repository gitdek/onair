// The live panel: a slim panel at the right edge of a display that floats
// above other windows, never takes focus, and is kept out of screen
// sharing. It shows a status line, answer cards and a few links, and a
// click on a link, or on a line of an answer, opens it in the browser. It decides nothing: Go says what it shows.
#import <AppKit/AppKit.h>

// OAPanelShow shows the panel at the right edge of a display, named as
// OADisplayID names it, or of the main display when there is no such
// display, with a line under its title, a status line, what a model said
// about the call, a line each, answer cards, and links. An answer card is
// rows, each a dictionary of strings, "kind", "text", "where" and "link":
// its question, its lines and a word on it. A link is a dictionary of
// strings, "kind", "title", "date", "why" and "link". It hides the panel
// when visible is NO. Main thread only.
void OAPanelShow(BOOL visible, NSString *display, NSString *headline, NSString *status, NSArray<NSString *> *calls, NSArray<NSDictionary<NSString *, NSString *> *> *answers,
                 NSArray<NSDictionary<NSString *, NSString *> *> *links);

// OAPanelContent lays out what the panel shows, at its width, without a
// window. OAPanelShow puts it in the panel.
NSView *OAPanelContent(NSString *headline, NSString *status, NSArray<NSString *> *calls, NSArray<NSDictionary<NSString *, NSString *> *> *answers,
                       NSArray<NSDictionary<NSString *, NSString *> *> *links);
