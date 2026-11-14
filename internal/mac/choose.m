#import "recorder.h"
#import "choose.h"
#import <ColorSync/ColorSync.h>
#import "_cgo_export.h"

NSString *OADisplayID(CGDirectDisplayID display) {
    // A display's number can change when it is plugged in again. Its UUID,
    // from its maker, model and serial number, doesn't.
    CFUUIDRef uuid = CGDisplayCreateUUIDFromDisplayID(display);
    if (!uuid) return [NSString stringWithFormat:@"display %u", display];
    NSString *text = CFBridgingRelease(CFUUIDCreateString(kCFAllocatorDefault, uuid));
    CFRelease(uuid);
    return text;
}

static CGDirectDisplayID displayOf(NSScreen *screen) {
    return [screen.deviceDescription[@"NSScreenNumber"] unsignedIntValue];
}

// Listing the displays through ScreenCaptureKit needs the screen-recording
// permission. NSScreen needs none, so the menu's list comes from here.
void OAReportDisplays(void) {
    NSArray<NSScreen *> *screens = NSScreen.screens;
    int count = (int)screens.count;
    onair_display *list = calloc(count > 0 ? count : 1, sizeof *list);
    NSMutableArray<NSString *> *kept = [NSMutableArray array]; // alive while Go copies them
    for (int i = 0; i < count; i++) {
        NSScreen *screen = screens[i];
        CGDirectDisplayID display = displayOf(screen);
        NSString *id = OADisplayID(display), *name = screen.localizedName.length ? screen.localizedName : @"Display";
        [kept addObject:id];
        [kept addObject:name];
        list[i] = (onair_display){.id = id.UTF8String, .name = name.UTF8String, .width = screen.frame.size.width, .height = screen.frame.size.height, .main = display == CGMainDisplayID()};
    }
    onairDisplays(list, count);
    free(list);
}

// OAOverlay is a borderless window over one display, which takes the keys.
@interface OAOverlay : NSWindow
@end

@implementation OAOverlay
- (BOOL)canBecomeKeyWindow {
    return YES;
}
@end

// OAAreaView dims its display and lets a person drag out an area of it.
// Escape cancels; Return, or letting go of the mouse, picks what was drawn.
@interface OAAreaView : NSView
@property(copy) void (^done)(NSRect area); // an empty area when cancelled
@end

@implementation OAAreaView {
    NSPoint _from;
    NSRect _area;
}

- (BOOL)acceptsFirstResponder {
    return YES;
}

- (BOOL)acceptsFirstMouse:(NSEvent *)event {
    return YES;
}

- (void)resetCursorRects {
    [self addCursorRect:self.bounds cursor:NSCursor.crosshairCursor];
}

- (void)mouseDown:(NSEvent *)event {
    _from = [self convertPoint:event.locationInWindow fromView:nil];
    _area = NSZeroRect;
    [self setNeedsDisplay:YES];
}

- (void)mouseDragged:(NSEvent *)event {
    NSPoint to = [self convertPoint:event.locationInWindow fromView:nil];
    NSRect drawn = NSMakeRect(MIN(_from.x, to.x), MIN(_from.y, to.y), fabs(to.x - _from.x), fabs(to.y - _from.y));
    _area = NSIntersectionRect(drawn, self.bounds);
    [self setNeedsDisplay:YES];
}

- (void)mouseUp:(NSEvent *)event {
    if (!NSIsEmptyRect(_area)) [self finish:_area];
}

- (void)keyDown:(NSEvent *)event {
    switch (event.keyCode) {
    case 53: // Escape
        [self finish:NSZeroRect];
        return;
    case 36: // Return
    case 76: // Enter
        if (!NSIsEmptyRect(_area)) [self finish:_area];
        return;
    }
    [super keyDown:event];
}

- (void)cancelOperation:(id)sender {
    [self finish:NSZeroRect];
}

- (void)finish:(NSRect)area {
    void (^done)(NSRect) = self.done;
    self.done = nil;
    if (done) done(area);
}

- (void)drawRect:(NSRect)dirty {
    [[NSColor colorWithWhite:0 alpha:0.45] setFill];
    NSRectFill(self.bounds);
    if (NSIsEmptyRect(_area)) return;
    [NSColor.clearColor setFill];
    NSRectFillUsingOperation(_area, NSCompositingOperationCopy);
    [[NSColor colorWithWhite:1 alpha:0.9] setStroke];
    NSBezierPath *edge = [NSBezierPath bezierPathWithRect:NSInsetRect(_area, 0.5, 0.5)];
    edge.lineWidth = 1;
    [edge stroke];
    NSString *size = [NSString stringWithFormat:@"%.0f × %.0f", _area.size.width, _area.size.height];
    NSDictionary *attributes = @{
        NSFontAttributeName : [NSFont monospacedDigitSystemFontOfSize:12 weight:NSFontWeightMedium],
        NSForegroundColorAttributeName : NSColor.whiteColor,
    };
    NSSize label = [size sizeWithAttributes:attributes];
    NSPoint at = NSMakePoint(NSMinX(_area) + 6, NSMinY(_area) - label.height - 4);
    if (at.y < 4) at.y = NSMinY(_area) + 6;
    [size drawAtPoint:at withAttributes:attributes];
}

@end

static OAOverlay *overlay;

void onair_select_area(const char *display) {
    NSString *wanted = [NSString stringWithUTF8String:display ?: ""];
    dispatch_async(dispatch_get_main_queue(), ^{
        if (overlay) return;
        NSScreen *screen = NSScreen.screens.firstObject;
        for (NSScreen *s in NSScreen.screens) {
            if ([OADisplayID(displayOf(s)) isEqualToString:wanted]) screen = s;
        }
        if (!screen) return;
        NSString *id = OADisplayID(displayOf(screen));
        NSRect frame = screen.frame;
        OAOverlay *window = [[OAOverlay alloc] initWithContentRect:frame styleMask:NSWindowStyleMaskBorderless backing:NSBackingStoreBuffered defer:NO];
        window.releasedWhenClosed = NO;
        window.opaque = NO;
        window.backgroundColor = NSColor.clearColor;
        window.level = NSScreenSaverWindowLevel;
        window.collectionBehavior = NSWindowCollectionBehaviorCanJoinAllSpaces | NSWindowCollectionBehaviorFullScreenAuxiliary;
        OAAreaView *view = [[OAAreaView alloc] initWithFrame:NSMakeRect(0, 0, frame.size.width, frame.size.height)];
        view.done = ^(NSRect area) {
            OAOverlay *closing = overlay;
            overlay = nil;
            [closing orderOut:nil];
            // Let the window go once the event that closed it is over.
            dispatch_async(dispatch_get_main_queue(), ^{
                [closing close];
            });
            // In the display's points from its top left, as capture takes them.
            if (!NSIsEmptyRect(area)) onairArea((char *)id.UTF8String, area.origin.x, frame.size.height - NSMaxY(area), area.size.width, area.size.height);
        };
        window.contentView = view;
        [window setFrame:frame display:YES];
        overlay = window;
        [NSApp activate];
        [window makeKeyAndOrderFront:nil];
        [window makeFirstResponder:view];
    });
}

BOOL OAWindowOpen(CGWindowID window) {
    if (!window) return NO;
    CFArrayRef list = CGWindowListCopyWindowInfo(kCGWindowListOptionIncludingWindow, window);
    BOOL open = list && CFArrayGetCount(list) > 0;
    if (list) CFRelease(list);
    return open;
}

// OAPicker shows the system's picker for one window, and keeps what it
// hands back: the only way to record that window, until On Air quits.
@interface OAPicker : NSObject <SCContentSharingPickerObserver>
@property(readonly) SCContentFilter *filter;
@property(readonly) CGWindowID window;
- (void)present;
- (void)forget;
@end

@implementation OAPicker

- (void)present {
    SCContentSharingPicker *picker = SCContentSharingPicker.sharedPicker;
    SCContentSharingPickerConfiguration *c = [[SCContentSharingPickerConfiguration alloc] init];
    c.allowedPickerModes = SCContentSharingPickerModeSingleWindow;
    c.allowsChangingSelectedContent = NO;
    NSString *own = NSBundle.mainBundle.bundleIdentifier;
    if (own) c.excludedBundleIDs = @[ own ];
    picker.defaultConfiguration = c;
    [picker addObserver:self];
    picker.active = YES;
    [picker presentPickerUsingContentStyle:SCShareableContentStyleWindow];
}

// Done puts the picker away, so the system offers no more of it.
- (void)done {
    SCContentSharingPicker *picker = SCContentSharingPicker.sharedPicker;
    [picker removeObserver:self];
    picker.active = NO;
}

- (void)forget {
    _filter = nil;
    _window = 0;
}

- (void)contentSharingPicker:(SCContentSharingPicker *)picker didUpdateWithFilter:(SCContentFilter *)filter forStream:(SCStream *)stream {
    dispatch_async(dispatch_get_main_queue(), ^{
        [self done];
        SCWindow *window = filter.includedWindows.firstObject;
        if (!window) {
            onairEvent(ONAIR_PICKER_FAILED, "it handed back no window.");
            return;
        }
        self->_filter = filter;
        self->_window = window.windowID;
        NSString *app = window.owningApplication.applicationName, *title = window.title;
        NSString *name = app.length && title.length ? [NSString stringWithFormat:@"%@ — %@", app, title] : (title.length ? title : app);
        onairWindow((char *)(name ?: @"").UTF8String);
    });
}

- (void)contentSharingPicker:(SCContentSharingPicker *)picker didCancelForStream:(SCStream *)stream {
    dispatch_async(dispatch_get_main_queue(), ^{
        [self done];
    });
}

- (void)contentSharingPickerStartDidFailWithError:(NSError *)error {
    NSString *why = error.localizedDescription ?: @"no reason was given.";
    dispatch_async(dispatch_get_main_queue(), ^{
        [self done];
        onairEvent(ONAIR_PICKER_FAILED, (char *)why.UTF8String);
    });
}

@end

static OAPicker *picker;

void onair_select_window(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (!picker) picker = [[OAPicker alloc] init];
        [picker present];
    });
}

SCContentFilter *OAPickedWindow(CGWindowID *window) {
    if (window) *window = picker.window;
    return picker.filter;
}

BOOL OAForgetClosedWindow(void) {
    if (!picker.filter || OAWindowOpen(picker.window)) return NO;
    [picker forget];
    return YES;
}
