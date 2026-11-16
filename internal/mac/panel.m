#import "panel.h"
#import "choose.h"

// The panel's measures, in points.
static const CGFloat panelWidth = 300, margin = 12, gap = 10;

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
    [self addCursorRect:self.bounds cursor:NSCursor.pointingHandCursor];
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

NSView *OAPanelContent(NSString *headline, NSString *status, NSArray<NSString *> *calls, NSArray<NSDictionary<NSString *, NSString *> *> *answers,
                       NSArray<NSDictionary<NSString *, NSString *> *> *links) {
    OAFlipped *content = [[OAFlipped alloc] initWithFrame:NSMakeRect(0, 0, panelWidth, 100)];
    CGFloat inner = panelWidth - 2 * margin, y = margin;
    y += OALabel(content, @"Live Context", [NSFont boldSystemFontOfSize:11], NSColor.secondaryLabelColor, margin, y, inner, 1) + 4;
    // Its digits keep their width, so the line doesn't shift as they count.
    CGFloat h = OALabel(content, headline, [NSFont monospacedDigitSystemFontOfSize:10 weight:NSFontWeightRegular], NSColor.secondaryLabelColor, margin, y, inner, 3);
    y += h > 0 && status.length ? h + 4 : h;
    y += OALabel(content, status, [NSFont systemFontOfSize:11], NSColor.secondaryLabelColor, margin, y, inner, 5);
    for (NSString *said in calls) {
        y += 6;
        y += OALabel(content, said, [NSFont systemFontOfSize:13 weight:NSFontWeightMedium], NSColor.labelColor, margin, y, inner, 2);
    }
    // What was said that differs from the record, with where the record
    // says it, and a click opens that moment. Then answer cards: a
    // question as heard, then its written answer and each of its sources,
    // or each line of its answer with where it was said under it,
    // indented, and a click opens that moment; or a word on it.
    CGFloat indent = 10, lineWidth = inner - indent;
    for (NSDictionary<NSString *, NSString *> *row in answers) {
        NSString *kind = row[@"kind"] ?: @"";
        if ([kind isEqualToString:@"differs"]) {
            y += gap;
            OALinkView *view = [[OALinkView alloc] initWithFrame:NSMakeRect(margin, y, inner, 10)];
            view.url = OAOpenable(row[@"link"]);
            view.toolTip = view.url.absoluteString;
            CGFloat h = OALabel(view, row[@"text"], [NSFont systemFontOfSize:12 weight:NSFontWeightMedium], NSColor.systemOrangeColor, 0, 0, inner, 5) + 1;
            h += OALabel(view, row[@"where"], [NSFont systemFontOfSize:10], NSColor.linkColor, 0, h, inner, 1);
            view.frame = NSMakeRect(margin, y, inner, h);
            [content addSubview:view];
            y += h;
        } else if ([kind isEqualToString:@"answer"]) {
            y += 4;
            y += OALabel(content, row[@"text"], [NSFont systemFontOfSize:12], NSColor.labelColor, margin + indent, y, lineWidth, 6);
        } else if ([kind isEqualToString:@"source"]) {
            y += 2;
            OALinkView *view = [[OALinkView alloc] initWithFrame:NSMakeRect(margin + indent, y, lineWidth, 10)];
            view.url = OAOpenable(row[@"link"]);
            view.toolTip = view.url.absoluteString;
            CGFloat h = OALabel(view, row[@"text"], [NSFont systemFontOfSize:10], NSColor.linkColor, 0, 0, lineWidth, 1);
            view.frame = NSMakeRect(margin + indent, y, lineWidth, h);
            [content addSubview:view];
            y += h;
        } else if ([kind isEqualToString:@"question"]) {
            y += gap;
            y += OALabel(content, row[@"text"], [NSFont systemFontOfSize:12 weight:NSFontWeightSemibold], NSColor.labelColor, margin, y, inner, 2);
        } else if ([kind isEqualToString:@"line"]) {
            y += 4;
            OALinkView *view = [[OALinkView alloc] initWithFrame:NSMakeRect(margin + indent, y, lineWidth, 10)];
            view.url = OAOpenable(row[@"link"]);
            view.toolTip = view.url.absoluteString;
            CGFloat h = OALabel(view, row[@"text"], [NSFont systemFontOfSize:12], NSColor.labelColor, 0, 0, lineWidth, 2) + 1;
            h += OALabel(view, row[@"where"], [NSFont systemFontOfSize:10], NSColor.linkColor, 0, h, lineWidth, 1);
            view.frame = NSMakeRect(margin + indent, y, lineWidth, h);
            [content addSubview:view];
            y += h;
        } else {
            y += 3;
            y += OALabel(content, row[@"text"], [NSFont systemFontOfSize:11], NSColor.secondaryLabelColor, margin + indent, y, lineWidth, 2);
        }
    }
    if (answers.count && links.count) {
        y += gap + 2;
        y += OALabel(content, @"Related", [NSFont boldSystemFontOfSize:10], NSColor.tertiaryLabelColor, margin, y, inner, 1) - gap + 2;
    }
    for (NSDictionary<NSString *, NSString *> *link in links) {
        y += gap;
        OALinkView *view = [[OALinkView alloc] initWithFrame:NSMakeRect(margin, y, inner, 10)];
        NSURL *url = [NSURL URLWithString:link[@"link"] ?: @""];
        view.url = OAOpenable(link[@"link"]);
        view.toolTip = url.absoluteString;
        NSString *kind = link[@"kind"] ?: @"", *date = link[@"date"] ?: @"";
        NSString *meta = kind.length && date.length ? [NSString stringWithFormat:@"%@ · %@", kind, date] : (kind.length ? kind : date);
        CGFloat h = 0;
        h += OALabel(view, meta, [NSFont systemFontOfSize:10], NSColor.tertiaryLabelColor, 0, h, inner, 1) + 1;
        h += OALabel(view, link[@"title"], [NSFont systemFontOfSize:13 weight:NSFontWeightSemibold], NSColor.labelColor, 0, h, inner, 2) + 1;
        h += OALabel(view, link[@"why"], [NSFont systemFontOfSize:11], NSColor.secondaryLabelColor, 0, h, inner, 3);
        view.frame = NSMakeRect(margin, y, inner, h);
        [content addSubview:view];
        y += h;
    }
    content.frame = NSMakeRect(0, 0, panelWidth, y + margin);
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

void OAPanelShow(BOOL visible, NSString *display, NSString *headline, NSString *status, NSArray<NSString *> *calls, NSArray<NSDictionary<NSString *, NSString *> *> *answers,
                 NSArray<NSDictionary<NSString *, NSString *> *> *links) {
    if (!visible) {
        [panel orderOut:nil];
        return;
    }
    NSPanel *p = OAPanel();
    NSView *content = OAPanelContent(headline, status, calls, answers, links);
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
