// Elephant macOS menu-bar companion (JXA). Runs as an osacompile applet.
// Input: the Go host atomically replaces the JSON model file named by
// ELEPHANT_MENU_STATE. Output: one private JSON event per stdout line:
// {"event":"ready"} or {"event":"action","id":...,"choice":...,"text":...}.
// Model strings are only displayed; they are never evaluated.
ObjC.import('Cocoa');
ObjC.bindFunction('getppid', ['int', []]);
ObjC.bindFunction('exit', ['void', ['int']]);

var initialParent = $.getppid();
var MAX_STATE_BYTES = 1048576;

function writeTo(handle, text) {
	handle.writeData($(text + '\n').dataUsingEncoding($.NSUTF8StringEncoding));
}
function emit(obj) { writeTo($.NSFileHandle.fileHandleWithStandardOutput, JSON.stringify(obj)); }
function logError(msg) { writeTo($.NSFileHandle.fileHandleWithStandardError, 'elephant menu: ' + msg); }
function str(v) { return typeof v === 'string' ? v : ''; }

// Preserve the original template silhouette for the menu-bar status item.
function elephantImage(attention) {
	var size = 18;
	var img = $.NSImage.alloc.initWithSize($.NSMakeSize(size, size));
	img.lockFocus;
	$.NSGraphicsContext.saveGraphicsState;
	$.NSColor.blackColor.set;
	var p = $.NSBezierPath.bezierPath;
	// body, head, and legs
	p.appendBezierPath($.NSBezierPath.bezierPathWithOvalInRect($.NSMakeRect(1.5, 5, 11, 8)));
	p.appendBezierPath($.NSBezierPath.bezierPathWithOvalInRect($.NSMakeRect(9.5, 7.5, 6.5, 6.5)));
	p.appendBezierPath($.NSBezierPath.bezierPathWithRect($.NSMakeRect(3, 2, 2.2, 5)));
	p.appendBezierPath($.NSBezierPath.bezierPathWithRect($.NSMakeRect(8.5, 2, 2.2, 5)));
	p.fill;
	var t = $.NSBezierPath.bezierPath;
	t.moveToPoint($.NSMakePoint(14.5, 9.5));
	t.curveToPointControlPoint1ControlPoint2($.NSMakePoint(16.2, 3), $.NSMakePoint(16.2, 7), $.NSMakePoint(15.8, 4.5));
	t.lineWidth = 1.8;
	t.lineCapStyle = $.NSLineCapStyleRound;
	t.stroke;
	$.NSGraphicsContext.currentContext.compositingOperation = $.NSCompositingOperationClear;
	var ear = $.NSBezierPath.bezierPathWithOvalInRect($.NSMakeRect(10.3, 9.3, 2.6, 3.4));
	ear.lineWidth = 0.9;
	ear.stroke;
	$.NSBezierPath.bezierPathWithOvalInRect($.NSMakeRect(13.6, 11, 1, 1)).fill;
	if (attention) {
		$.NSBezierPath.bezierPathWithOvalInRect($.NSMakeRect(0, 11, 7, 7)).fill;
		$.NSGraphicsContext.currentContext.compositingOperation = $.NSCompositingOperationSourceOver;
		$.NSBezierPath.bezierPathWithOvalInRect($.NSMakeRect(1, 12, 5, 5)).fill;
	}
	$.NSGraphicsContext.restoreGraphicsState;
	img.unlockFocus;
	img.template = true;
	return img;
}

var appliedRevision = -1;
var pending = null;       // newest model not yet applied
var shownDialogs = {};    // dialog IDs already displayed
var inDialog = false;
var attentionShown = null;
var target, item, button;

ObjC.registerSubclass({
	name: 'ElephantMenuTarget',
	superclass: 'NSObject',
	methods: {
		'choose:': {
			types: ['void', ['id']],
			implementation: function (sender) {
				var id = ObjC.unwrap(sender.representedObject);
				if (typeof id === 'string' && id !== '') emit({ event: 'action', id: id });
			}
		},
		'hideMenuBar:': {
			types: ['void', ['id']],
			implementation: function (sender) {
				emit({ event: 'action', id: 'hide' });
				$.NSApp.terminate(null);
			}
		},
		'checkParent:': {
			types: ['void', ['id']],
			implementation: function (timer) {
				var pp = $.getppid();
				// exit directly: terminate: may be deferred inside a modal alert
				if (pp !== initialParent || pp === 1) $.exit(0);
			}
		},
		'poll:': {
			types: ['void', ['id']],
			implementation: function (timer) {
				try { poll(); } catch (e) { logError(String(e)); }
			}
		}
	}
});

function readState() {
	var path = ObjC.unwrap($.NSProcessInfo.processInfo.environment.objectForKey($('ELEPHANT_MENU_STATE')));
	if (typeof path !== 'string' || path === '') return null;
	var data = $.NSData.dataWithContentsOfFile($(path));
	if (data.isNil() || data.length > MAX_STATE_BYTES) return null;
	var text = ObjC.unwrap($.NSString.alloc.initWithDataEncoding(data, $.NSUTF8StringEncoding));
	if (typeof text !== 'string') return null;
	var model = JSON.parse(text);
	if (!model || typeof model.revision !== 'number') return null;
	return model;
}

function poll() {
	if (inDialog) return;
	var model = readState();
	if (model && model.revision > appliedRevision && (!pending || model.revision > pending.revision)) pending = model;
	if (!pending) return;
	var m = pending;
	pending = null;
	appliedRevision = m.revision;
	apply(m);
	var d = m.dialog;
	if (d && typeof d.id === 'string' && d.id !== '' && !shownDialogs[d.id]) {
		shownDialogs[d.id] = true;
		showDialog(d);
	}
}

function addItems(menu, items) {
	if (!Array.isArray(items)) return;
	for (var i = 0; i < items.length; i++) {
		var it = items[i];
		if (!it || typeof it !== 'object') continue;
		if (it.separator) { menu.addItem($.NSMenuItem.separatorItem); continue; }
		var hasSub = Array.isArray(it.items) && it.items.length > 0;
		var actionable = !hasSub && str(it.id) !== '';
		var row = $.NSMenuItem.alloc.initWithTitleActionKeyEquivalent($(str(it.title)), actionable ? 'choose:' : null, $(''));
		if (hasSub) {
			var sub = $.NSMenu.alloc.initWithTitle($(str(it.title)));
			sub.autoenablesItems = false;
			addItems(sub, it.items);
			row.submenu = sub;
			row.enabled = it.enabled !== false;
		} else if (actionable) {
			row.target = target;
			row.representedObject = $(it.id);
			row.enabled = it.enabled === true;
		} else {
			row.enabled = false;
		}
		if (it.checked) row.state = 1; // NSControlStateValueOn
		menu.addItem(row);
	}
}

function buildMenu(items) {
	var menu = $.NSMenu.alloc.initWithTitle($('Elephant'));
	menu.autoenablesItems = false;
	addItems(menu, items);
	if (menu.numberOfItems > 0) menu.addItem($.NSMenuItem.separatorItem);
	var hide = $.NSMenuItem.alloc.initWithTitleActionKeyEquivalent($('Hide Menu Bar'), 'hideMenuBar:', $(''));
	hide.target = target;
	hide.enabled = true;
	menu.addItem(hide);
	return menu;
}

function apply(m) {
	item.menu = buildMenu(m.items);
	button.toolTip = $(str(m.tooltip) || 'Elephant');
	var attention = m.attention === true;
	if (attention !== attentionShown) {
		button.image = elephantImage(attention);
		attentionShown = attention;
	}
}

var dialogLogo = null;

function showDialog(d) {
	var buttons = Array.isArray(d.buttons) ? d.buttons.filter(function (b) {
		return b && typeof b.id === 'string' && b.id !== '';
	}) : [];
	if (buttons.length === 0) buttons = [{ id: 'ok', title: 'OK' }];
	var alert = $.NSAlert.alloc.init;
	if (!dialogLogo) {
		var path = $.NSBundle.mainBundle.pathForResourceOfType('dialog-logo', 'png');
		dialogLogo = $.NSImage.alloc.initWithContentsOfFile(path);
	}
	alert.icon = dialogLogo;
	alert.messageText = $(str(d.title));
	alert.informativeText = $(str(d.message));
	for (var i = 0; i < buttons.length; i++) {
		var b = alert.addButtonWithTitle($(str(buttons[i].title) || buttons[i].id));
		if (buttons[i].id === d.default) b.keyEquivalent = $('\r');
		else if (buttons[i].id === d.cancel) b.keyEquivalent = $('\x1b');
		else b.keyEquivalent = $('');
	}
	var field = null;
	if (d.input) {
		field = $.NSTextField.alloc.initWithFrame($.NSMakeRect(0, 0, 280, 24));
		field.stringValue = $(str(d.value));
		alert.accessoryView = field;
		alert.window.initialFirstResponder = field;
	}
	inDialog = true;
	var response;
	try {
		$.NSApp.activateIgnoringOtherApps(true);
		response = alert.runModal;
	} finally {
		inDialog = false;
	}
	var index = response - 1000; // NSAlertFirstButtonReturn
	var choice = index >= 0 && index < buttons.length ? buttons[index].id : str(d.cancel);
	var ev = { event: 'action', id: d.id, choice: choice };
	if (field) ev.text = ObjC.unwrap(field.stringValue);
	emit(ev);
}

var app = $.NSApplication.sharedApplication;
app.setActivationPolicy($.NSApplicationActivationPolicyAccessory);
app.finishLaunching;

target = $.ElephantMenuTarget.alloc.init;
item = $.NSStatusBar.systemStatusBar.statusItemWithLength($.NSVariableStatusItemLength);
button = item.button;
apply({ revision: -1, tooltip: 'Elephant is starting', attention: false,
	items: [{ title: 'Elephant is starting…', enabled: false }] });

// Common modes: orphan cleanup must also run while a menu or alert is open.
var parentTimer = $.NSTimer.timerWithTimeIntervalTargetSelectorUserInfoRepeats(1.0, target, 'checkParent:', null, true);
$.NSRunLoop.currentRunLoop.addTimerForMode(parentTimer, $.NSRunLoopCommonModes);
// Default mode: state polling pauses during menu tracking and modal alerts.
var pollTimer = $.NSTimer.scheduledTimerWithTimeIntervalTargetSelectorUserInfoRepeats(0.25, target, 'poll:', null, true);

// Keep native objects reachable for the whole run loop.
var keepAlive = [app, target, item, parentTimer, pollTimer];

if (!item.isNil() && !button.isNil()) {
	emit({ event: 'ready' });
}

app.run;
