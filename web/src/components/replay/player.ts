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

import type {ReplayScreen} from "@/components/replay/screens";
import type {Recording} from "@/lib/recording";

// While nothing happens for longer than this, skipping idle time jumps ahead.
const IdleGap = 2000;

/** Plays a recording on a screen in recorded time, at a speed, skipping idle
 *  time if asked. A screen is drawn from its first frame on, so seeking back
 *  replays the recording from the start up to the new position at once. */
export class ReplayPlayer {
  position = 0;
  playing = false;
  speed = 1;
  skipIdle = true;

  private index = 0;
  private last = 0;
  private raf = 0;
  private lastUpdate = 0;

  constructor(private recording: Recording, private screen: ReplayScreen, private onUpdate: () => void) {}

  get duration() {
    return this.recording.duration;
  }

  play() {
    if (this.playing) {
      return;
    }
    if (this.position >= this.duration) {
      this.seek(0);
    }
    this.playing = true;
    this.last = performance.now();
    this.raf = requestAnimationFrame(this.tick);
    this.onUpdate();
  }

  pause() {
    this.playing = false;
    cancelAnimationFrame(this.raf);
    this.onUpdate();
  }

  seek(position: number) {
    position = Math.max(0, Math.min(position, this.duration));
    if (position < this.position) {
      this.screen.reset();
      this.index = 0;
    }
    this.position = position;
    this.drawUntil(position);
    this.onUpdate();
  }

  dispose() {
    this.playing = false;
    cancelAnimationFrame(this.raf);
  }

  private tick = (now: number) => {
    if (!this.playing) {
      return;
    }

    let position = this.position + (now - this.last) * this.speed;
    this.last = now;
    const next = this.recording.frames[this.index];
    if (this.skipIdle && next && next.time - this.position > IdleGap) {
      position = Math.max(position, next.time - 500);
    }
    this.position = Math.min(position, this.duration);
    this.drawUntil(this.position);

    if (this.position >= this.duration) {
      this.playing = false;
      this.onUpdate();
      return;
    }
    if (now - this.lastUpdate > 100) {
      this.lastUpdate = now;
      this.onUpdate();
    }
    this.raf = requestAnimationFrame(this.tick);
  };

  // drawUntil hands the screen every frame up to the position; screens that
  // draw later keep the frames in order themselves.
  private drawUntil(position: number) {
    const frames = this.recording.frames;
    while (this.index < frames.length && frames[this.index].time <= position) {
      const frame = frames[this.index++];
      void this.screen.apply(frame.kind, frame.data);
    }
  }
}
