// Copyright 2024 The casbin Authors. All Rights Reserved.
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

export function debounce<T extends (...args: any[]) => void>(fn: T, delay = 500) {
  let timer: ReturnType<typeof setTimeout> | null = null;
  return (...args: Parameters<T>) => {
    if (timer) {
      clearTimeout(timer);
    }
    timer = setTimeout(() => {
      timer = null;
      fn(...args);
    }, delay);
  };
}

export function toggleFullscreen() {
  if (document.fullscreenElement) {
    document.exitFullscreen?.();
  } else {
    document.documentElement.requestFullscreen?.();
  }
}

/** The props every connection view takes, whether it fills the window or a workbench tab. */
export interface ConnectionProps {
  assetId: string;
  /** set inside the workbench, where the view shares the window with the tab bar */
  inTab?: boolean;
  username?: string;
  password?: string;
  /** lets the workbench close the connection when its tab is closed */
  addClient?: (client: {disconnect: () => void}) => void;
  /** closes the workbench tab, or the window when the view has it to itself */
  onClose?: () => void;
}

export function closeView(props: ConnectionProps) {
  if (props.onClose) {
    props.onClose();
  } else {
    window.close();
  }
}

/** Key combinations a browser cannot send itself, as X11 keysyms. */
export const HotKeys: {label: string; keys: number[]}[] = [
  {label: "Ctrl+Alt+Delete", keys: [0xffe3, 0xffe9, 0xffff]},
  {label: "Ctrl+Alt+Backspace", keys: [0xffe3, 0xffe9, 0xff08]},
  {label: "Windows+D", keys: [0xffeb, 0x64]},
  {label: "Windows+E", keys: [0xffeb, 0x65]},
  {label: "Windows+R", keys: [0xffeb, 0x72]},
  {label: "Windows+X", keys: [0xffeb, 0x78]},
  {label: "Windows", keys: [0xffeb]},
];
