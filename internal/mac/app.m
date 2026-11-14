#import "recorder.h"
#import <AppKit/AppKit.h>
#import "_cgo_export.h"

// OAApp is the menu-bar item: a dot, grey when idle and red with a running
// clock while recording, and a menu. It keeps no state of its own beyond
// what Go last told it to show.
@interface OAApp : NSObject <NSApplicationDelegate, NSMenuDelegate>
- (void)show:(BOOL)recording last:(NSString *)last note:(NSString *)note;
@end

@implementation OAApp {
    NSStatusItem *_item;
    NSTimer *_clock;
    NSDate *_since;
    BOOL _recording;
    NSString *_last, *_note;
}

- (void)applicationDidFinishLaunching:(NSNotification *)notification {
    _item = [[NSStatusBar systemStatusBar] statusItemWithLength:NSVariableStatusItemLength];
    NSMenu *menu = [[NSMenu alloc] init];
    menu.delegate = self;
    menu.autoenablesItems = NO;
    _item.menu = menu;
    [self draw];
    onairMenu(0); // the app is up
}

- (void)show:(BOOL)recording last:(NSString *)last note:(NSString *)note {
    if (recording && !_recording) {
        _since = [NSDate date];
        _clock = [NSTimer scheduledTimerWithTimeInterval:1 target:self selector:@selector(draw) userInfo:nil repeats:YES];
    }
    if (!recording && _recording) {
        [_clock invalidate];
        _clock = nil;
    }
    _recording = recording;
    _last = last.length ? last : nil;
    _note = note.length ? note : nil;
    [self draw];
}

- (void)draw {
    NSMutableAttributedString *title = [[NSMutableAttributedString alloc] initWithString:@"●" attributes:@{
        NSForegroundColorAttributeName : _recording ? NSColor.systemRedColor : NSColor.tertiaryLabelColor,
        NSFontAttributeName : [NSFont systemFontOfSize:13],
    }];
    if (_recording) {
        long seconds = (long)[[NSDate date] timeIntervalSinceDate:_since];
        NSString *clock = seconds >= 3600 ? [NSString stringWithFormat:@" %ld:%02ld:%02ld", seconds / 3600, seconds / 60 % 60, seconds % 60]
                                          : [NSString stringWithFormat:@" %02ld:%02ld", seconds / 60, seconds % 60];
        [title appendAttributedString:[[NSAttributedString alloc] initWithString:clock attributes:@{
            NSFontAttributeName : [NSFont monospacedDigitSystemFontOfSize:12 weight:NSFontWeightRegular],
        }]];
    }
    _item.button.attributedTitle = title;
    _item.button.toolTip = _recording ? @"On Air is recording" : @"On Air";
}

- (void)menuNeedsUpdate:(NSMenu *)menu {
    [menu removeAllItems];
    [self add:menu title:_recording ? @"Stop Recording" : @"Start Recording" action:_recording ? ONAIR_MENU_STOP : ONAIR_MENU_START enabled:YES];
    if (_note) {
        [menu addItem:[NSMenuItem separatorItem]];
        // A long note wraps over a few lines instead of widening the menu.
        NSMutableArray<NSString *> *lines = [NSMutableArray array];
        NSMutableString *line = [NSMutableString string];
        for (NSString *word in [_note componentsSeparatedByString:@" "]) {
            if (line.length && line.length + word.length > 52) {
                [lines addObject:[line copy]];
                [line setString:@""];
            }
            [line appendFormat:line.length ? @" %@" : @"%@", word];
        }
        if (line.length) [lines addObject:[line copy]];
        for (NSString *l in lines) [self add:menu title:l action:0 enabled:NO];
    }
    [menu addItem:[NSMenuItem separatorItem]];
    if (_last) [self add:menu title:[NSString stringWithFormat:@"Show “%@”", _last.lastPathComponent] action:ONAIR_MENU_LAST enabled:YES];
    [self add:menu title:@"Open Recordings Folder" action:ONAIR_MENU_FOLDER enabled:YES];
    [menu addItem:[NSMenuItem separatorItem]];
    [self add:menu title:@"Quit On Air" action:ONAIR_MENU_QUIT enabled:YES];
}

- (void)add:(NSMenu *)menu title:(NSString *)title action:(int)action enabled:(BOOL)enabled {
    NSMenuItem *item = [[NSMenuItem alloc] initWithTitle:title action:@selector(chosen:) keyEquivalent:@""];
    item.target = self;
    item.tag = action;
    item.enabled = enabled;
    [menu addItem:item];
}

- (void)chosen:(NSMenuItem *)item {
    onairMenu((int)item.tag);
}

@end

static OAApp *app;

void onair_run(void) {
    @autoreleasepool {
        NSApplication *application = [NSApplication sharedApplication];
        application.activationPolicy = NSApplicationActivationPolicyAccessory;
        app = [[OAApp alloc] init];
        application.delegate = app;
        [application run];
    }
}

void onair_show(int recording, const char *last, const char *note) {
    NSString *l = [NSString stringWithUTF8String:last ?: ""], *n = [NSString stringWithUTF8String:note ?: ""];
    dispatch_async(dispatch_get_main_queue(), ^{
        [app show:recording != 0 last:l note:n];
    });
}

void onair_reveal(const char *path) {
    NSURL *url = [NSURL fileURLWithPath:[NSString stringWithUTF8String:path]];
    dispatch_async(dispatch_get_main_queue(), ^{
        NSNumber *directory = nil;
        [url getResourceValue:&directory forKey:NSURLIsDirectoryKey error:nil];
        if (directory.boolValue) {
            [[NSWorkspace sharedWorkspace] openURL:url];
        } else {
            [[NSWorkspace sharedWorkspace] activateFileViewerSelectingURLs:@[ url ]];
        }
    });
}

void onair_quit(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        [NSApp terminate:nil];
    });
}
