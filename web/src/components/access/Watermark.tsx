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

import * as React from "react";
import dayjs from "dayjs";

function escapeXml(text: string) {
  return text.replace(/[&<>"']/g, (c) => `&#${c.charCodeAt(0)};`);
}

/** A tile with the text twice, so that the repeated tiles stagger like bricks. */
function getTileUrl(text: string, time: string) {
  const width = Math.min(Math.max(text.length * 8 + 80, 280), 560);
  const height = Math.round(width * 0.55);
  const mark = (x: number, y: number) =>
    `<g transform="translate(${x} ${y}) rotate(-20)" text-anchor="middle" font-family="system-ui, sans-serif" font-size="13" fill="#888" fill-opacity="0.22">` +
    `<text y="-4">${escapeXml(text)}</text><text y="14">${time}</text></g>`;
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${width * 2}" height="${height * 2}">` +
    mark(width / 2, height / 2) + mark(width * 1.5, height * 1.5) + "</svg>";
  return `url("data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}")`;
}

function useMinute() {
  const [time, setTime] = React.useState(() => dayjs().format("YYYY-MM-DD HH:mm"));
  React.useEffect(() => {
    const timer = setInterval(() => setTime(dayjs().format("YYYY-MM-DD HH:mm")), 10000);
    return () => clearInterval(timer);
  }, []);
  return time;
}

/** Covers a session with its watermark, the user and client IP from the server and the current time;
 *  it lets clicks and keys through to the session below. */
export function Watermark({text}: {text: string}) {
  const time = useMinute();
  const backgroundImage = React.useMemo(() => getTileUrl(text, time), [text, time]);
  return <div aria-hidden className="pointer-events-none absolute inset-0 z-20 select-none" style={{backgroundImage}} />;
}
