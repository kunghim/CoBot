package browser

import (
	"context"
	"fmt"
	"runtime"
	"strings"
)

// keyDef describes a named key the way the browser's input events want it.
type keyDef struct {
	key, code string
	vk        int
	text      string // what typing it inserts, for keys that insert something
}

var namedKeys = map[string]keyDef{
	"Enter":      {"Enter", "Enter", 13, "\r"},
	"Escape":     {"Escape", "Escape", 27, ""},
	"Tab":        {"Tab", "Tab", 9, ""},
	"Backspace":  {"Backspace", "Backspace", 8, ""},
	"Delete":     {"Delete", "Delete", 46, ""},
	"Space":      {" ", "Space", 32, " "},
	"ArrowUp":    {"ArrowUp", "ArrowUp", 38, ""},
	"ArrowDown":  {"ArrowDown", "ArrowDown", 40, ""},
	"ArrowLeft":  {"ArrowLeft", "ArrowLeft", 37, ""},
	"ArrowRight": {"ArrowRight", "ArrowRight", 39, ""},
	"Home":       {"Home", "Home", 36, ""},
	"End":        {"End", "End", 35, ""},
	"PageUp":     {"PageUp", "PageUp", 33, ""},
	"PageDown":   {"PageDown", "PageDown", 34, ""},
}

const (
	modAlt   = 1
	modCtrl  = 2
	modMeta  = 4
	modShift = 8
)

var modifierNames = map[string]int{
	"alt": modAlt, "option": modAlt, "control": modCtrl, "ctrl": modCtrl,
	"meta": modMeta, "command": modMeta, "cmd": modMeta, "shift": modShift,
}

// editing commands the browser runs for a shortcut; headless Chrome does not
// map the key chord to them by itself.
var shortcutCommands = map[string]string{
	"a": "selectAll", "c": "copy", "x": "cut", "v": "paste", "z": "undo", "y": "redo",
}

// parseKey reads "Enter", "a", "Control+a" or "Shift+Tab".
func parseKey(spec string) (keyDef, int, error) {
	parts := strings.Split(strings.TrimSpace(spec), "+")
	if spec == "" || len(parts) == 0 {
		return keyDef{}, 0, fmt.Errorf("%w: key needs a key name", ErrBadRequest)
	}
	// "+" itself is the last part of "Control++".
	name := parts[len(parts)-1]
	mods := 0
	for _, p := range parts[:len(parts)-1] {
		m, ok := modifierNames[strings.ToLower(p)]
		if !ok {
			return keyDef{}, 0, fmt.Errorf("%w: unknown modifier %q", ErrBadRequest, p)
		}
		mods |= m
	}
	if name == "" {
		name = "+"
	}
	if def, ok := namedKeys[name]; ok {
		return def, mods, nil
	}
	if len([]rune(name)) == 1 {
		r := []rune(name)[0]
		def := keyDef{key: name, text: name}
		switch {
		case r >= 'a' && r <= 'z':
			def.code, def.vk = "Key"+strings.ToUpper(name), int(r-'a')+'A'
		case r >= 'A' && r <= 'Z':
			def.code, def.vk = "Key"+name, int(r)
			mods |= modShift
		case r >= '0' && r <= '9':
			def.code, def.vk = "Digit"+name, int(r)
		default:
			def.code = ""
		}
		return def, mods, nil
	}
	return keyDef{}, 0, fmt.Errorf("%w: unknown key %q (use Enter, Escape, Tab, Backspace, Delete, Space, arrows, Home, End, PageUp, PageDown or a single character)", ErrBadRequest, name)
}

func (r *callRun) pressKey(ctx context.Context, spec string) (string, error) {
	def, mods, err := parseKey(spec)
	if err != nil {
		return "", err
	}
	loads, navs := r.cur.counters()
	text := def.text
	// A chord is a shortcut, not typing.
	if mods&(modCtrl|modAlt|modMeta) != 0 {
		text = ""
	}
	down := map[string]any{"type": "rawKeyDown", "modifiers": mods, "key": def.key, "code": def.code, "windowsVirtualKeyCode": def.vk}
	if text != "" {
		down["type"] = "keyDown"
		down["text"] = text
		down["unmodifiedText"] = def.text
	}
	if mods&(modCtrl|modMeta) != 0 {
		// Command on a Mac, Control elsewhere.
		primary := modCtrl
		if runtime.GOOS == "darwin" {
			primary = modMeta
		}
		if command, ok := shortcutCommands[strings.ToLower(def.key)]; ok && mods&primary != 0 {
			down["commands"] = []string{command}
		}
	}
	if err := r.cur.cdp(ctx, "Input.dispatchKeyEvent", down, nil); err != nil {
		return "", err
	}
	up := map[string]any{"type": "keyUp", "modifiers": mods, "key": def.key, "code": def.code, "windowsVirtualKeyCode": def.vk}
	if err := r.cur.cdp(ctx, "Input.dispatchKeyEvent", up, nil); err != nil {
		return "", err
	}
	if def.key == "Enter" || def.key == "Tab" || def.key == "Escape" {
		r.cur.settle(ctx, loads, navs)
	}
	return "已按 " + spec, nil
}
