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
import i18next from "i18next";
import dayjs from "dayjs";
import {useParams, useSearchParams} from "react-router-dom";
import {Download, Film, Pause, Play} from "lucide-react";
import * as CommandBackend from "@/backend/CommandBackend";
import * as SessionBackend from "@/backend/SessionBackend";
import {Button} from "@/components/ui/button";
import {Switch} from "@/components/ui/switch";
import {ReplayPlayer} from "@/components/replay/player";
import {createScreen} from "@/components/replay/screens";
import {formatDuration, parseRecording, type Recording} from "@/lib/recording";
import * as Setting from "@/lib/setting";

const Speeds = [1, 2, 4, 8, 16];

interface CommandMark {
  time: number;
  text: string;
  action: string;
}

/** Replays a recorded session in the window, with the commands of a terminal
 *  session beside it to jump to. */
export default function SessionReplayPage() {
  const {owner = "", name = ""} = useParams();
  const [searchParams, setSearchParams] = useSearchParams();
  const index = Number(searchParams.get("index") ?? 0) || 0;
  const sessionId = `${owner}/${name}`;

  const containerRef = React.useRef<HTMLDivElement>(null);
  const playerRef = React.useRef<ReplayPlayer | null>(null);
  const [recordings, setRecordings] = React.useState<string[]>([]);
  const [recording, setRecording] = React.useState<Recording | null>(null);
  const [progress, setProgress] = React.useState(0);
  const [error, setError] = React.useState("");
  const [commands, setCommands] = React.useState<CommandMark[]>([]);
  const [, setTick] = React.useState(0);

  React.useEffect(() => {
    document.title = `${i18next.t("session:Replay")} | ${name}`;
    SessionBackend.getSession(owner, name).then((res) => {
      if (res.status === "ok" && res.data) {
        setRecordings(res.data.recordings ?? []);
      }
    });
  }, [owner, name]);

  React.useEffect(() => {
    let active = true;
    setRecording(null);
    setError("");
    setProgress(0);
    SessionBackend.downloadRecording(sessionId, index, (loaded) => active && setProgress(loaded))
      .then((buffer) => {
        if (active) {
          setRecording(parseRecording(buffer));
        }
      })
      .catch((e) => active && setError(String(e?.message ?? e)));
    return () => {
      active = false;
    };
  }, [sessionId, index]);

  React.useEffect(() => {
    if (!recording || !containerRef.current) {
      return;
    }
    const screen = createScreen(containerRef.current, recording.header);
    const player = new ReplayPlayer(recording, screen, () => setTick((tick) => tick + 1));
    playerRef.current = player;
    player.play();
    return () => {
      player.dispose();
      screen.dispose();
      playerRef.current = null;
    };
  }, [recording]);

  React.useEffect(() => {
    if (!recording || (recording.header.protocol !== "SSH" && recording.header.protocol !== "Telnet")) {
      setCommands([]);
      return;
    }
    const start = dayjs(recording.header.startTime);
    CommandBackend.getCommands(owner, {page: 1, pageSize: 1000, field: "session", value: sessionId, sortField: "createdTime", sortOrder: "ascend"}).then((res) => {
      if (res.status !== "ok") {
        return;
      }
      setCommands((res.data ?? [])
        .filter((command: any) => command.session === sessionId)
        // command times are in whole seconds, the recording starts at a millisecond
        .map((command: any) => ({time: Math.max(0, dayjs(command.createdTime).diff(start)), text: command.text, action: command.action}))
        .filter((command: CommandMark) => command.time <= recording.duration + 1000));
    });
  }, [recording, owner, sessionId]);

  const player = playerRef.current;
  const header = recording?.header;
  const isTerminal = header?.protocol === "SSH" || header?.protocol === "Telnet";
  const downloadUrl = (format?: string) =>
    `${Setting.ServerUrl}/api/get-session-recording?id=${encodeURIComponent(sessionId)}&index=${index}${format ? `&format=${format}` : ""}`;

  return (
    <div className="flex h-screen w-screen flex-col bg-[#1b1b1b] text-neutral-200">
      <div className="flex flex-wrap items-center gap-3 border-b border-neutral-700 px-4 py-2 text-sm">
        <Film className="h-4 w-4" />
        <span className="font-medium">{i18next.t("session:Replay")}</span>
        <span className="truncate text-neutral-400">
          {header ? `${header.asset} | ${header.user} | ${Setting.getFormattedDate(header.startTime)}` : sessionId}
        </span>
        {recordings.length > 1 ? (
          <span className="flex items-center gap-1">
            {recordings.map((_, i) => (
              <Button
                key={i}
                size="sm"
                variant={i === index ? "secondary" : "ghost"}
                className="h-7 px-2"
                onClick={() => setSearchParams({index: String(i)})}
              >
                {i + 1}
              </Button>
            ))}
          </span>
        ) : null}
        <span className="ml-auto flex items-center gap-2">
          <Button size="sm" variant="ghost" className="h-7" asChild>
            <a href={downloadUrl()}><Download className="h-4 w-4" />{i18next.t("session:Download")}</a>
          </Button>
          {isTerminal ? (
            <Button size="sm" variant="ghost" className="h-7" asChild>
              <a href={downloadUrl("cast")}><Download className="h-4 w-4" />asciicast</a>
            </Button>
          ) : null}
        </span>
      </div>

      <div className="flex min-h-0 flex-1">
        <div className="relative min-w-0 flex-1">
          <div ref={containerRef} className={isTerminal ? "h-full w-full overflow-auto p-2" : "h-full w-full"} />
          {!recording ? (
            <div className="absolute inset-0 flex items-center justify-center text-sm text-neutral-400">
              {error || `${i18next.t("session:Loading the recording")}... ${(progress / 1024 / 1024).toFixed(1)} MB`}
            </div>
          ) : null}
        </div>
        {commands.length > 0 ? (
          <div className="w-72 shrink-0 overflow-auto border-l border-neutral-700 text-xs">
            <div className="sticky top-0 bg-[#1b1b1b] px-3 py-2 font-medium text-neutral-300">{i18next.t("general:Commands")}</div>
            {commands.map((command, i) => (
              <button
                key={i}
                type="button"
                className="flex w-full gap-2 px-3 py-1.5 text-left hover:bg-neutral-800"
                onClick={() => player?.seek(Math.max(0, command.time - 1000))}
              >
                <span className="shrink-0 tabular-nums text-neutral-500">{formatDuration(command.time)}</span>
                <span className={`break-all font-mono ${command.action === "Block" ? "text-red-400" : command.action === "Alert" ? "text-amber-300" : ""}`}>
                  {command.text}
                </span>
              </button>
            ))}
          </div>
        ) : null}
      </div>

      <div className="flex items-center gap-3 border-t border-neutral-700 px-4 py-2 text-sm">
        <Button
          size="sm"
          variant="secondary"
          className="h-8 w-8 p-0"
          disabled={!player}
          onClick={() => (player?.playing ? player.pause() : player?.play())}
          aria-label={player?.playing ? i18next.t("session:Pause") : i18next.t("session:Play")}
        >
          {player?.playing ? <Pause className="h-4 w-4" /> : <Play className="h-4 w-4" />}
        </Button>
        <span className="w-28 shrink-0 tabular-nums text-neutral-400">
          {formatDuration(player?.position ?? 0)} / {formatDuration(recording?.duration ?? 0)}
        </span>
        <input
          type="range"
          className="min-w-0 flex-1 accent-white"
          min={0}
          max={recording?.duration || 0}
          step={100}
          value={player?.position ?? 0}
          disabled={!player}
          onChange={(e) => player?.seek(Number(e.target.value))}
        />
        <select
          className="h-8 rounded border border-neutral-600 bg-neutral-800 px-2 text-neutral-200"
          value={player?.speed ?? 1}
          disabled={!player}
          onChange={(e) => {
            if (player) {
              player.speed = Number(e.target.value);
              setTick((tick) => tick + 1);
            }
          }}
        >
          {Speeds.map((speed) => <option key={speed} value={speed}>{speed}x</option>)}
        </select>
        <label className="flex shrink-0 items-center gap-2 text-neutral-300">
          <Switch
            className="data-[state=checked]:bg-sky-500 data-[state=unchecked]:bg-neutral-600"
            checked={player?.skipIdle ?? true}
            disabled={!player}
            onCheckedChange={(checked) => {
              if (player) {
                player.skipIdle = checked;
                setTick((tick) => tick + 1);
              }
            }}
          />
          {i18next.t("session:Skip idle time")}
        </label>
      </div>
    </div>
  );
}
