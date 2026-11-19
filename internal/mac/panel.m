#import "panel.h"
#import "choose.h"

// The panel's measures, in points: wide enough for a short line of
// readable text and its sources beside it.
static const CGFloat panelWidth = 340, margin = 14, gap = 12, chipColumn = 54;

// OAFlipped lays out from its top left, as text is read.
@interface OAFlipped : NSView
@end
@implementation OAFlipped
- (BOOL)isFlipped {
    return YES;
}
@end

// OALinkView is one link: a click on it, anywhere, opens it. It takes the
// first click even though the panel never becomes active.
@interface OALinkView : OAFlipped
@property(copy) NSURL *url;
@end

@implementation OALinkView
- (BOOL)acceptsFirstMouse:(NSEvent *)event {
    return YES;
}
- (BOOL)mouseDownCanMoveWindow {
    return NO;
}
- (NSView *)hitTest:(NSPoint)point {
    // The labels inside take no clicks of their own.
    return [super hitTest:point] ? self : nil;
}
- (void)mouseDown:(NSEvent *)event {
}
- (void)mouseUp:(NSEvent *)event {
    NSPoint at = [self convertPoint:event.locationInWindow fromView:nil];
    if (self.url && NSPointInRect(at, self.bounds)) [[NSWorkspace sharedWorkspace] openURL:self.url];
}
- (void)resetCursorRects {
    if (self.url) [self addCursorRect:self.bounds cursor:NSCursor.pointingHandCursor];
}
@end

// Label is text that wraps at width, over lines at most, with an ellipsis
// where it is cut, placed at y. It returns its height.
static CGFloat OALabel(NSView *into, NSString *text, NSFont *font, NSColor *color, CGFloat x, CGFloat y, CGFloat width, NSInteger lines) {
    if (!text.length) return 0;
    NSTextField *label = [NSTextField wrappingLabelWithString:text];
    label.font = font;
    label.textColor = color;
    label.maximumNumberOfLines = lines;
    label.lineBreakMode = NSLineBreakByWordWrapping;
    label.cell.truncatesLastVisibleLine = YES;
    label.preferredMaxLayoutWidth = width;
    label.selectable = NO;
    NSSize fit = [label sizeThatFits:NSMakeSize(width, CGFLOAT_MAX)];
    CGFloat line = ceil(font.ascender - font.descender + font.leading);
    CGFloat height = MIN(ceil(fit.height), lines * line + 4);
    label.frame = NSMakeRect(x, y, width, height);
    [into addSubview:label];
    return height;
}

// OAOpenable is a link's URL when a browser can open it, http or https.
static NSURL *OAOpenable(NSString *link) {
    NSURL *url = [NSURL URLWithString:link ?: @""];
    return [url.scheme isEqualToString:@"http"] || [url.scheme isEqualToString:@"https"] ? url : nil;
}

// OAChip is a source: a small rounded label, such as "Nov 9", that a click
// opens, at the right of a column of width at x, y. It returns its height.
static CGFloat OAChip(NSView *into, NSString *text, NSString *link, CGFloat x, CGFloat y, CGFloat width) {
    if (!text.length) return 0;
    NSFont *font = [NSFont systemFontOfSize:10.5 weight:NSFontWeightMedium];
    NSTextField *label = [NSTextField labelWithString:text];
    label.font = font;
    label.textColor = NSColor.linkColor;
    label.lineBreakMode = NSLineBreakByTruncatingTail;
    NSSize fit = [label sizeThatFits:NSMakeSize(width, CGFLOAT_MAX)];
    CGFloat w = MIN(ceil(fit.width) + 12, width), h = ceil(fit.height) + 3;
    OALinkView *chip = [[OALinkView alloc] initWithFrame:NSMakeRect(x + width - w, y, w, h)];
    chip.url = OAOpenable(link);
    chip.toolTip = chip.url.absoluteString;
    NSBox *pill = [[NSBox alloc] initWithFrame:chip.bounds];
    pill.boxType = NSBoxCustom;
    pill.borderWidth = 0;
    pill.cornerRadius = h / 2;
    pill.fillColor = [NSColor.linkColor colorWithAlphaComponent:0.12];
    [chip addSubview:pill];
    label.frame = NSMakeRect(6, 1, w - 12, h - 3);
    [chip addSubview:label];
    [into addSubview:chip];
    return h;
}

// OACardBackground puts a card's quiet background behind what was laid out
// between top and bottom.
static void OACardBackground(NSView *into, CGFloat top, CGFloat bottom) {
    NSBox *card = [[NSBox alloc] initWithFrame:NSMakeRect(margin - 8, top - 8, panelWidth - 2 * margin + 16, bottom - top + 14)];
    card.boxType = NSBoxCustom;
    card.borderWidth = 0;
    card.cornerRadius = 9;
    card.fillColor = [NSColor.labelColor colorWithAlphaComponent:0.06];
    [into addSubview:card positioned:NSWindowBelow relativeTo:nil];
}

NSView *OAPanelContent(NSString *status, NSArray<NSString *> *calls, NSArray<NSDictionary<NSString *, NSString *> *> *rows) {
    OAFlipped *content = [[OAFlipped alloc] initWithFrame:NSMakeRect(0, 0, panelWidth, 100)];
    CGFloat inner = panelWidth - 2 * margin, y = margin;
    // The title and a quiet line on how it goes.
    y += OALabel(content, @"Live Context", [NSFont systemFontOfSize:11 weight:NSFontWeightSemibold], NSColor.tertiaryLabelColor, margin, y, inner, 1) + 2;
    y += OALabel(content, status, [NSFont systemFontOfSize:11], NSColor.secondaryLabelColor, margin, y, inner, 3);
    for (NSString *said in calls) {
        y += 8;
        y += OALabel(content, said, [NSFont systemFontOfSize:13 weight:NSFontWeightMedium], NSColor.labelColor, margin, y, inner, 2);
    }
    // What was said that differs from the record, and where the record
    // says it, a click opening that moment. Then the cards, each in a
    // quiet box: its title, a word on it, if any, then a short line a fact, with its sources at
    // its right, a click on one opening it.
    CGFloat cardTop = -1;
    NSUInteger i = 0;
    while (i < rows.count) {
        NSDictionary<NSString *, NSString *> *row = rows[i];
        NSString *kind = row[@"kind"] ?: @"";
        if ([kind isEqualToString:@"differs"]) {
            y += gap + 4;
            OALinkView *view = [[OALinkView alloc] initWithFrame:NSMakeRect(margin, y, inner, 10)];
            view.url = OAOpenable(row[@"link"]);
            view.toolTip = view.url.absoluteString;
            CGFloat h = OALabel(view, row[@"text"], [NSFont systemFontOfSize:13 weight:NSFontWeightMedium], NSColor.systemOrangeColor, 0, 0, inner, 5) + 2;
            h += OALabel(view, row[@"where"], [NSFont systemFontOfSize:11], NSColor.linkColor, 0, h, inner, 1);
            view.frame = NSMakeRect(margin, y, inner, h);
            [content addSubview:view];
            y += h;
        } else if ([kind isEqualToString:@"title"]) {
            if (cardTop >= 0) OACardBackground(content, cardTop, y);
            y += gap + 10;
            cardTop = y;
            y += OALabel(content, row[@"text"], [NSFont systemFontOfSize:15 weight:NSFontWeightSemibold], NSColor.labelColor, margin, y, inner, 2);
        } else if ([kind isEqualToString:@"line"]) {
            NSUInteger j = i + 1;
            NSMutableArray<NSDictionary<NSString *, NSString *> *> *chips = [NSMutableArray array];
            while (j < rows.count && [rows[j][@"kind"] isEqualToString:@"source"]) [chips addObject:rows[j++]];
            CGFloat textWidth = chips.count ? inner - chipColumn - 6 : inner;
            y += 6;
            CGFloat h = OALabel(content, row[@"text"], [NSFont systemFontOfSize:13.5], NSColor.labelColor, margin, y, textWidth, 3);
            CGFloat c = 1;
            for (NSDictionary<NSString *, NSString *> *chip in chips) c += OAChip(content, chip[@"text"], chip[@"link"], margin + inner - chipColumn, y + c, chipColumn) + 3;
            y += MAX(h, c - 3);
            i = j;
            continue;
        } else if ([kind isEqualToString:@"note"]) {
            y += 5;
            y += OALabel(content, row[@"text"], [NSFont systemFontOfSize:12], NSColor.secondaryLabelColor, margin, y, inner, 2);
        }
        i++;
    }
    if (cardTop >= 0) OACardBackground(content, cardTop, y);
    content.frame = NSMakeRect(0, 0, panelWidth, y + margin + (cardTop >= 0 ? 6 : 0));
    return content;
}

// RoundedMask is the shape of the panel's background: a rectangle with
// rounded corners, stretched to fit.
static NSImage *OARoundedMask(CGFloat radius) {
    CGFloat side = radius * 2 + 1;
    NSImage *mask = [NSImage imageWithSize:NSMakeSize(side, side) flipped:NO drawingHandler:^BOOL(NSRect rect) {
        [NSColor.blackColor setFill];
        [[NSBezierPath bezierPathWithRoundedRect:rect xRadius:radius yRadius:radius] fill];
        return YES;
    }];
    mask.capInsets = NSEdgeInsetsMake(radius, radius, radius, radius);
    mask.resizingMode = NSImageResizingModeStretch;
    return mask;
}

// ScreenFor is the screen of a display named as OADisplayID names it, or
// the main display's, the one with the menu bar.
static NSScreen *OAScreenFor(NSString *display) {
    NSScreen *main = nil;
    for (NSScreen *screen in NSScreen.screens) {
        CGDirectDisplayID id = [screen.deviceDescription[@"NSScreenNumber"] unsignedIntValue];
        if (display.length && [OADisplayID(id) isEqualToString:display]) return screen;
        if (id == CGMainDisplayID()) main = screen;
    }
    return main ?: NSScreen.screens.firstObject;
}

static NSPanel *panel;
static NSVisualEffectView *background;
static NSString *placedOn; // the display the panel was last put beside

static NSPanel *OAPanel(void) {
    if (panel) return panel;
    panel = [[NSPanel alloc] initWithContentRect:NSMakeRect(0, 0, panelWidth, 100)
                                       styleMask:NSWindowStyleMaskBorderless | NSWindowStyleMaskNonactivatingPanel
                                         backing:NSBackingStoreBuffered
                                           defer:NO];
    panel.floatingPanel = YES;
    panel.level = NSFloatingWindowLevel;
    panel.becomesKeyOnlyIfNeeded = YES;
    // An accessory app is never active, and a panel that hid when it isn't
    // would never show.
    panel.hidesOnDeactivate = NO;
    panel.releasedWhenClosed = NO;
    panel.worksWhenModal = YES;
    // Kept out of what other apps capture, such as a call's screen share.
    // On Air's own recording leaves it out by its filter as well.
    panel.sharingType = NSWindowSharingNone;
    panel.collectionBehavior = NSWindowCollectionBehaviorCanJoinAllSpaces | NSWindowCollectionBehaviorFullScreenAuxiliary |
                               NSWindowCollectionBehaviorStationary | NSWindowCollectionBehaviorIgnoresCycle;
    panel.movableByWindowBackground = YES;
    panel.opaque = NO;
    panel.backgroundColor = NSColor.clearColor;
    panel.hasShadow = YES;
    panel.title = @"Live Context";
    background = [[NSVisualEffectView alloc] initWithFrame:NSMakeRect(0, 0, panelWidth, 100)];
    background.material = NSVisualEffectMaterialPopover;
    background.blendingMode = NSVisualEffectBlendingModeBehindWindow;
    background.state = NSVisualEffectStateActive;
    background.maskImage = OARoundedMask(10);
    panel.contentView = background;
    return panel;
}

void OAPanelShow(BOOL visible, NSString *display, NSString *status, NSArray<NSString *> *calls, NSArray<NSDictionary<NSString *, NSString *> *> *rows) {
    if (!visible) {
        [panel orderOut:nil];
        return;
    }
    NSPanel *p = OAPanel();
    NSView *content = OAPanelContent(status, calls, rows);
    NSRect size = content.frame;
    background.frame = size;
    background.subviews = @[ content ];
    // Beside a display it wasn't beside, the panel goes to its top right
    // corner. Otherwise its top right corner stays where it is, even if it
    // was dragged, and it grows or shrinks downwards.
    NSRect frame = p.frame;
    CGFloat right = NSMaxX(frame), top = NSMaxY(frame);
    NSString *on = display ?: @"";
    if (!p.visible || !placedOn || ![placedOn isEqualToString:on]) {
        NSRect visibleFrame = OAScreenFor(on).visibleFrame;
        right = NSMaxX(visibleFrame) - margin;
        top = NSMaxY(visibleFrame) - margin;
        placedOn = on;
    }
    [p setFrame:NSMakeRect(right - size.size.width, top - size.size.height, size.size.width, size.size.height) display:YES];
    [p invalidateCursorRectsForView:content];
    [p orderFrontRegardless];
}
