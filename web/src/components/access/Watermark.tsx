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
import {cn} from "@/lib/utils";

export interface WatermarkProps {
  text: string;
  className?: string;
}

function formatTimestamp(date: Date): string {
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

function escapeXml(value: string): string {
  return value
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&apos;");
}

function buildWatermarkDataUri(label: string, timestamp: string): string {
  const safeLabel = escapeXml(label);
  const safeTime = escapeXml(timestamp);
  const maxChars = Math.max(label.length, timestamp.length, 16);
  const cellWidth = Math.max(260, Math.min(560, maxChars * 9 + 110));
  const cellHeight = Math.max(150, Math.round(cellWidth * 0.52));
  const tileWidth = cellWidth * 2;
  const tileHeight = cellHeight * 2;
  const font = "system-ui, -apple-system, BlinkMacSystemFont, sans-serif";

  const renderMark = (cx: number, cy: number) =>
    `<g transform="translate(${cx} ${cy}) rotate(-22)" text-anchor="middle" font-family="${font}" fill="#828282">` +
    `<text y="-5" font-size="13" font-weight="500" fill-opacity="0.16" letter-spacing="0.02em">${safeLabel}</text>` +
    `<text y="13" font-size="11.5" fill-opacity="0.13">${safeTime}</text>` +
    "</g>";

  const svg =
    `<svg xmlns="http://www.w3.org/2000/svg" width="${tileWidth}" height="${tileHeight}">` +
    renderMark(cellWidth * 0.5, cellHeight * 0.5) +
    renderMark(cellWidth * 1.5, cellHeight * 1.5) +
    "</svg>";

  return `url("data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}")`;
}

function useMinuteTimestamp(): string {
  const [timestamp, setTimestamp] = React.useState(() => formatTimestamp(new Date()));

  React.useEffect(() => {
    let timer: ReturnType<typeof setTimeout>;
    const schedule = () => {
      const now = new Date();
      const delay = (60 - now.getSeconds()) * 1000 - now.getMilliseconds() + 50;
      timer = setTimeout(() => {
        setTimestamp(formatTimestamp(new Date()));
        schedule();
      }, Math.max(1000, delay));
    };
    schedule();
    return () => clearTimeout(timer);
  }, []);

  return timestamp;
}

/** Tiled diagonal session watermark showing the active user and current time. */
export function Watermark({text, className}: WatermarkProps) {
  const timestamp = useMinuteTimestamp();
  const label = text.trim();

  const backgroundImage = React.useMemo(
    () => (label ? buildWatermarkDataUri(label, timestamp) : ""),
    [label, timestamp],
  );

  if (!label) {
    return null;
  }

  return (
    <div
      aria-hidden="true"
      className={cn("pointer-events-none absolute inset-0 z-20 select-none", className)}
      style={{backgroundImage, backgroundRepeat: "repeat"}}
    />
  );
}
