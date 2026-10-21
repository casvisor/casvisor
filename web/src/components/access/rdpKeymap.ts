// Copyright 2026 The casbin Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

/** Maps a browser KeyboardEvent.code to a PC scancode (set 1 make code) and
 *  whether it needs the extended (E0) prefix, which is what RDP expects. */

type Key = {code: number; extended: boolean};

const base: Record<string, number> = {
  Escape: 0x01,
  Digit1: 0x02, Digit2: 0x03, Digit3: 0x04, Digit4: 0x05, Digit5: 0x06,
  Digit6: 0x07, Digit7: 0x08, Digit8: 0x09, Digit9: 0x0a, Digit0: 0x0b,
  Minus: 0x0c, Equal: 0x0d, Backspace: 0x0e, Tab: 0x0f,
  KeyQ: 0x10, KeyW: 0x11, KeyE: 0x12, KeyR: 0x13, KeyT: 0x14, KeyY: 0x15,
  KeyU: 0x16, KeyI: 0x17, KeyO: 0x18, KeyP: 0x19,
  BracketLeft: 0x1a, BracketRight: 0x1b, Enter: 0x1c, ControlLeft: 0x1d,
  KeyA: 0x1e, KeyS: 0x1f, KeyD: 0x20, KeyF: 0x21, KeyG: 0x22, KeyH: 0x23,
  KeyJ: 0x24, KeyK: 0x25, KeyL: 0x26, Semicolon: 0x27, Quote: 0x28, Backquote: 0x29,
  ShiftLeft: 0x2a, Backslash: 0x2b,
  KeyZ: 0x2c, KeyX: 0x2d, KeyC: 0x2e, KeyV: 0x2f, KeyB: 0x30, KeyN: 0x31, KeyM: 0x32,
  Comma: 0x33, Period: 0x34, Slash: 0x35, ShiftRight: 0x36,
  NumpadMultiply: 0x37, AltLeft: 0x38, Space: 0x39, CapsLock: 0x3a,
  F1: 0x3b, F2: 0x3c, F3: 0x3d, F4: 0x3e, F5: 0x3f, F6: 0x40, F7: 0x41,
  F8: 0x42, F9: 0x43, F10: 0x44, NumLock: 0x45, ScrollLock: 0x46,
  Numpad7: 0x47, Numpad8: 0x48, Numpad9: 0x49, NumpadSubtract: 0x4a,
  Numpad4: 0x4b, Numpad5: 0x4c, Numpad6: 0x4d, NumpadAdd: 0x4e,
  Numpad1: 0x4f, Numpad2: 0x50, Numpad3: 0x51, Numpad0: 0x52, NumpadDecimal: 0x53,
  F11: 0x57, F12: 0x58,
};

const extended: Record<string, number> = {
  ControlRight: 0x1d, AltRight: 0x38, NumpadEnter: 0x1c, NumpadDivide: 0x35,
  Home: 0x47, ArrowUp: 0x48, PageUp: 0x49, ArrowLeft: 0x4b, ArrowRight: 0x4d,
  End: 0x4f, ArrowDown: 0x50, PageDown: 0x51, Insert: 0x52, Delete: 0x53,
  MetaLeft: 0x5b, MetaRight: 0x5c, ContextMenu: 0x5d,
};

export function codeToScancode(code: string): Key | null {
  if (code in base) {
    return {code: base[code], extended: false};
  }
  if (code in extended) {
    return {code: extended[code], extended: true};
  }
  return null;
}

// The X11 keysyms the hot-key menu uses, mapped to scancodes.
const keysyms: Record<number, Key> = {
  0xffe3: {code: 0x1d, extended: false}, // Left Ctrl
  0xffe9: {code: 0x38, extended: false}, // Left Alt
  0xffff: {code: 0x53, extended: true},  // Delete
  0xff08: {code: 0x0e, extended: false}, // Backspace
  0xffeb: {code: 0x5b, extended: true},  // Left Windows
  0x64: {code: 0x20, extended: false},   // d
  0x65: {code: 0x12, extended: false},   // e
  0x72: {code: 0x13, extended: false},   // r
  0x78: {code: 0x2d, extended: false},   // x
};

export function keysymToScancode(keysym: number): Key | null {
  return keysyms[keysym] ?? null;
}
