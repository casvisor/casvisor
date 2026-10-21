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

/** What guacd's status codes mean, as the antd page spelled them out. */
export function getGuacdErrorMessage(code: number, message: string) {
  const messages: Record<number, string> = {
    256: "Unsupported access",
    512: "Remote service exception, please check if the target device can be accessed normally.",
    513: "Server busy",
    514: "Server connection timed out",
    515: "Remote service exception",
    516: "Resource not found",
    517: "Resource conflict",
    518: "Resource closed",
    519: "Remote service not found",
    520: "Remote service unavailable",
    521: "Session conflict",
    522: "Session connection timed out",
    523: "Session closed",
    768: "Network unreachable",
    769: "Server password authentication failed",
    771: "Client is forbidden",
    776: "Client connection timed out",
    781: "Client exception",
    783: "Incorrect request type",
    800: "Session does not exist",
    801: "Failed to create tunnel, please check if Guacd service is functioning correctly.",
    802: "Admin forcefully closed this session",
    804: `Check that the parameters (width,height) are in the correct format: ${message}`,
    805: `Failed to find the asset: ${message}`,
    806: `Failed to update session status: ${message}`,
  };
  return messages[code];
}
