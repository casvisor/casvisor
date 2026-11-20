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
  /** a one-time ticket for credentials typed in the Connect dialog */
  ticket?: string;
  /** asks for the credentials again, as a ticket only works once; null when cancelled */
  requestConnectTicket?: () => Promise<string | null>;
  /** set when a password was removed from the address bar of an old link */
  legacyPasswordRemoved?: boolean;
  /** the account of the asset to connect as, the asset's own account when empty */
  account?: string;
  /** lets the workbench close the connection when its tab is closed */
  addClient?: (client: {disconnect: () => void}) => void;
  /** closes the workbench tab, or the window when the view has it to itself */
  onClose?: () => void;
}

const legacyPasswordRemovedPrefix = "casvisorLegacyPasswordRemoved:";
let takenLegacyPassword: {assetId: string; removed: boolean} | undefined;

/** Removes legacy passwords from the URL without using or retaining their values. */
export function scrubLegacyAccessPassword(pathname: string, search: string): string {
  const params = new URLSearchParams(search);
  if (!params.has("password")) {
    return pathname + search;
  }

  params.delete("password");
  const rest = params.toString();
  const cleanedSearch = rest ? `?${rest}` : "";
  window.history.replaceState(window.history.state, "", `${pathname}${cleanedSearch}${window.location.hash}`);
  const match = /^\/access\/([^/]+)\/([^/]+)\/?$/.exec(pathname);
  if (match) {
    try {
      const assetId = `${decodeURIComponent(match[1])}/${decodeURIComponent(match[2])}`;
      sessionStorage.setItem(legacyPasswordRemovedPrefix + assetId, "1");
    } catch {
      // The URL is already clean even if the notice cannot be stored.
    }
  }
  return pathname + cleanedSearch;
}

/** Takes the notice from this visit or from before sign-in. */
export function takeLegacyPasswordRemoved(assetId: string): boolean {
  const {pathname, search} = window.location;
  let removed = scrubLegacyAccessPassword(pathname, search) !== pathname + search;
  try {
    const key = legacyPasswordRemovedPrefix + assetId;
    if (sessionStorage.getItem(key) === "1") {
      removed = true;
      sessionStorage.removeItem(key);
    }
  } catch {
    // Still report a password removed from the current URL.
  }
  if (removed) {
    takenLegacyPassword = {assetId, removed: true};
  } else if (takenLegacyPassword?.assetId !== assetId) {
    takenLegacyPassword = undefined;
  }
  return takenLegacyPassword?.removed ?? false;
}

let takenConnectTicket: {assetId: string; ticket: string} | undefined;

export function clearConnectTicket(assetId: string) {
  if (takenConnectTicket?.assetId === assetId) {
    takenConnectTicket = undefined;
  }
}

/** Removes the ticket from the address bar and keeps it for repeated renders of this asset. */
export function takeConnectTicket(assetId: string): string {
  const {pathname, search, hash} = window.location;
  const params = new URLSearchParams(hash.slice(1));
  if (params.has("ticket")) {
    const ticket = params.get("ticket") ?? "";
    params.delete("ticket");
    const rest = params.toString();
    const nextUrl = `${pathname}${search}${rest ? `#${rest}` : ""}`;
    window.history.replaceState(window.history.state, "", nextUrl);
    takenConnectTicket = ticket ? {assetId, ticket} : undefined;
    return ticket;
  }

  if (takenConnectTicket?.assetId !== assetId) {
    takenConnectTicket = undefined;
  }
  return takenConnectTicket?.ticket ?? "";
}

/** The query a tunnel signs in with: a ticket, else the username of a direct link. The ticket
 *  the page was opened with is spent by the first connection, so it is dropped here. */
export function takeTunnelCredentials(assetId: string, initialTicket: {current: string}, ticket?: string, username?: string): Record<string, string> {
  const wsTicket = ticket ?? initialTicket.current;
  if (initialTicket.current) {
    initialTicket.current = "";
    clearConnectTicket(assetId);
  }
  if (wsTicket) {
    return {ticket: wsTicket};
  }
  return username ? {username} : {};
}

/** Holds a Web Lock until the returned function is called. Chrome freezes a
 *  page that has been in the background for a while, which closes its
 *  WebSockets, but it does not freeze a page holding a lock. */
export function holdPageLock(): () => void {
  let released = false;
  let release = () => {
    released = true;
  };
  navigator.locks?.request(`casvisor-connection-${Math.random()}`, () => {
    if (released) {
      return undefined;
    }
    return new Promise<void>((resolve) => {
      release = resolve;
    });
  }).catch(() => undefined);
  return () => release();
}

const entryScript = /\/static\/index-[\w-]+\.js/;

/** Reloads a connection page when the server has a newer frontend than the
 *  one it runs, which happens when the server is upgraded while the page stays
 *  open, and resolves whether it did. The workbench is left alone, as
 *  reloading it would close its other tabs. */
export async function reloadIfUpdated(props: ConnectionProps): Promise<boolean> {
  if (props.inTab) {
    return false;
  }
  const current = Array.from(document.scripts).map((s) => s.getAttribute("src") ?? "").find((src) => entryScript.test(src));
  if (!current) {
    return false;
  }
  try {
    const res = await fetch("/", {cache: "no-store"});
    const latest = res.ok ? (await res.text()).match(entryScript)?.[0] : undefined;
    if (!latest || current.endsWith(latest)) {
      return false;
    }
  } catch {
    return false;
  }
  window.location.reload();
  return true;
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
