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

package controllers

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"image/png"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/casvisor/casvisor/object"
	gordp "github.com/casvisor/gordp"
)

// Binary messages that carry clipboard files rather than the screen.
const (
	rdpFrameClipboardFile = 3 // gateway to browser: [3][u32 id][bytes of a copied file]
	rdpFrameClipboardData = 4 // browser to gateway: [4][bytes of the files being pasted]
)

const (
	clipboardChunkSize     = 1 << 20
	clipboardMaxImage      = 64 << 20
	clipboardMaxPasteBytes = 2 << 30
	clipboardMaxPasteFiles = 10000
)

var sessionClipboardsDir = filepath.Join("data", "clipboard")

// ClearSessionClipboards removes the files pasted into sessions that ended
// with the process.
func ClearSessionClipboards() error {
	return os.RemoveAll(sessionClipboardsDir)
}

type clipboardFileInfo struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	Dir  bool   `json:"dir,omitempty"`
}

// rdpClipboard carries text, images and files between the browser and the
// remote clipboard of one RDP session. The gordp client is only touched on
// the goroutine pumping it, through run.
type rdpClipboard struct {
	c       *ApiController
	asset   *object.Asset
	stream  *wsStream
	client  *gordp.Client
	ops     chan func()
	done    chan struct{}
	dir     string
	canCopy bool
	// files may be copied out of, or pasted into, the remote host
	canDownload bool
	canUpload   bool

	// the remote host's files, read on the pumping goroutine
	remoteFiles []gordp.ClipboardFile
	downloads   sync.Mutex

	// what the browser is pasting, read on the input goroutine
	upload *clipboardUpload
	// what is on offer to the remote host, owned by the pumping goroutine
	offered    []*os.File
	offeredDir string
	offers     int
}

type clipboardUpload struct {
	offer   *gordp.Clipboard
	dir     string
	files   []*os.File
	current int
	written int64
}

// run executes op on the pumping goroutine, waiting for room in the queue.
func (r *rdpClipboard) run(op func()) bool {
	select {
	case r.ops <- op:
		return true
	case <-r.done:
		return false
	}
}

// remoteChanged is called on the pumping goroutine with what the remote host
// copied, and tells the browser.
func (r *rdpClipboard) remoteChanged(data *gordp.Clipboard) {
	r.remoteFiles = nil
	msg := rdpMessage{Type: "clipboard", Text: data.Text}
	var details []string
	if data.Text != "" {
		details = append(details, truncateRunes(data.Text, 1000))
	}
	if r.canDownload && len(data.Files) > 0 {
		r.remoteFiles = data.Files
		names := make([]string, 0, len(data.Files))
		for _, f := range data.Files {
			msg.Files = append(msg.Files, clipboardFileInfo{Name: strings.ReplaceAll(f.Name, "\\", "/"), Size: f.Size, Dir: f.Dir})
			names = append(names, f.Name)
		}
		details = append(details, "files: "+truncateRunes(strings.Join(names, ", "), 1000))
	}
	if data.Image == nil {
		if msg.Text != "" || len(msg.Files) > 0 {
			_ = r.stream.WriteJSONUnrecorded(msg)
			r.c.auditRdp(r.asset, "clipboard-copy", strings.Join(details, "\n"))
		}
		return
	}

	size := data.Image.Bounds().Size()
	details = append(details, fmt.Sprintf("image: %dx%d", size.X, size.Y))
	// encoding a large image takes a while, so the screen keeps going meanwhile
	go func() {
		encoded := data.PNG
		if encoded == nil {
			var buf bytes.Buffer
			if err := tileEncoder.Encode(&buf, data.Image); err != nil {
				return
			}
			encoded = buf.Bytes()
		}
		msg.Png = base64.StdEncoding.EncodeToString(encoded)
		_ = r.stream.WriteJSONUnrecorded(msg)
	}()
	r.c.auditRdp(r.asset, "clipboard-copy", strings.Join(details, "\n"))
}

// download sends the remote host's copied files to the browser: one file as
// it is, several or a directory as a zip.
func (r *rdpClipboard) download(id int) {
	if !r.canDownload {
		return
	}
	got := make(chan []gordp.ClipboardFile, 1)
	if !r.run(func() { got <- r.remoteFiles }) {
		return
	}
	var files []gordp.ClipboardFile
	select {
	case files = <-got:
	case <-r.done:
		return
	}
	if len(files) == 0 {
		_ = r.stream.WriteJSONUnrecorded(rdpMessage{Type: "clipfile-end", Id: id, Message: "The remote clipboard has no files"})
		return
	}

	// one download at a time, as the remote host reads one file list
	r.downloads.Lock()
	defer r.downloads.Unlock()

	err := r.sendFiles(id, files)
	msg := rdpMessage{Type: "clipfile-end", Id: id}
	if err != nil {
		msg.Message = err.Error()
	}
	_ = r.stream.WriteJSONUnrecorded(msg)
}

func (r *rdpClipboard) sendFiles(id int, files []gordp.ClipboardFile) error {
	out := &clipboardFileWriter{stream: r.stream, id: id}
	single := len(files) == 1 && !files[0].Dir
	name := fmt.Sprintf("%s-clipboard.zip", r.asset.Name)
	if single {
		name = path.Base(strings.ReplaceAll(files[0].Name, "\\", "/"))
	}
	start := rdpMessage{Type: "clipfile-start", Id: id, Name: name, Size: -1}
	if single {
		start.Size = files[0].Size
	}
	if err := r.stream.WriteJSONUnrecorded(start); err != nil {
		return err
	}

	var names []string
	var total int64
	if single {
		n, err := r.copyFile(out, 0, files[0])
		if err != nil {
			return err
		}
		names, total = append(names, files[0].Name), n
	} else {
		zw := zip.NewWriter(out)
		for i, f := range files {
			entry := strings.ReplaceAll(f.Name, "\\", "/")
			header := &zip.FileHeader{Name: entry, Method: zip.Deflate, Modified: f.WriteTime}
			if f.WriteTime.IsZero() {
				header.Modified = time.Now()
			}
			if f.Dir {
				header.Name += "/"
				header.Method = zip.Store
			}
			w, err := zw.CreateHeader(header)
			if err != nil {
				return err
			}
			if f.Dir {
				continue
			}
			n, err := r.copyFile(w, i, f)
			if err != nil {
				return err
			}
			names, total = append(names, f.Name), total+n
		}
		if err := zw.Close(); err != nil {
			return err
		}
	}
	if err := out.Flush(); err != nil {
		return err
	}
	r.c.auditRdp(r.asset, "clipboard-download", fmt.Sprintf("%s (%d bytes)", truncateRunes(strings.Join(names, ", "), 1000), total))
	return nil
}

// copyFile reads file index of the remote clipboard into w.
func (r *rdpClipboard) copyFile(w io.Writer, index int, f gordp.ClipboardFile) (int64, error) {
	size := f.Size
	if size < 0 {
		var err error
		if size, err = r.remoteFileSize(index); err != nil {
			return 0, err
		}
	}
	var offset int64
	for offset < size {
		chunk, err := r.readRemoteFile(index, offset, int(min(int64(clipboardChunkSize), size-offset)))
		if err != nil {
			return offset, fmt.Errorf("%s: %w", f.Name, err)
		}
		if len(chunk) == 0 {
			break
		}
		if _, err := w.Write(chunk); err != nil {
			return offset, err
		}
		offset += int64(len(chunk))
	}
	return offset, nil
}

type clipboardRead struct {
	data []byte
	size int64
	err  error
}

var errClipboardClosed = errors.New("the session ended")

func (r *rdpClipboard) await(start func(done chan<- clipboardRead)) clipboardRead {
	result := make(chan clipboardRead, 1)
	if !r.run(func() { start(result) }) {
		return clipboardRead{err: errClipboardClosed}
	}
	select {
	case res := <-result:
		return res
	case <-r.done:
		return clipboardRead{err: errClipboardClosed}
	case <-time.After(time.Minute):
		return clipboardRead{err: errors.New("the remote host stopped sending the file")}
	}
}

func (r *rdpClipboard) readRemoteFile(index int, offset int64, size int) ([]byte, error) {
	res := r.await(func(done chan<- clipboardRead) {
		err := r.client.ReadClipboardFile(index, offset, size, func(data []byte, err error) {
			done <- clipboardRead{data: data, err: err}
		})
		if err != nil {
			done <- clipboardRead{err: err}
		}
	})
	return res.data, res.err
}

func (r *rdpClipboard) remoteFileSize(index int) (int64, error) {
	res := r.await(func(done chan<- clipboardRead) {
		err := r.client.ClipboardFileSize(index, func(size int64, err error) {
			done <- clipboardRead{size: size, err: err}
		})
		if err != nil {
			done <- clipboardRead{err: err}
		}
	})
	return res.size, res.err
}

// clipboardFileWriter sends what is written to it to the browser in chunks.
type clipboardFileWriter struct {
	stream *wsStream
	id     int
	buf    []byte
}

func (w *clipboardFileWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for len(w.buf) >= clipboardChunkSize {
		if err := w.send(w.buf[:clipboardChunkSize]); err != nil {
			return 0, err
		}
		w.buf = w.buf[clipboardChunkSize:]
	}
	return len(p), nil
}

func (w *clipboardFileWriter) Flush() error {
	if len(w.buf) == 0 {
		return nil
	}
	err := w.send(w.buf)
	w.buf = nil
	return err
}

func (w *clipboardFileWriter) send(p []byte) error {
	frame := make([]byte, 5+len(p))
	frame[0] = rdpFrameClipboardFile
	binary.LittleEndian.PutUint32(frame[1:], uint32(w.id))
	copy(frame[5:], p)
	return w.stream.writeBinary(frame)
}

// paste takes what the browser pastes: text and an image at once, and files,
// whose bytes follow in binary messages. It runs on the input goroutine.
func (r *rdpClipboard) paste(msg *rdpMessage) error {
	r.abortUpload()
	offer := &gordp.Clipboard{Text: msg.Text}
	var details []string
	if msg.Text != "" {
		details = append(details, truncateRunes(msg.Text, 1000))
	}
	if msg.Png != "" {
		if len(msg.Png) > clipboardMaxImage {
			return errors.New("the image is too large")
		}
		data, err := base64.StdEncoding.DecodeString(msg.Png)
		if err != nil {
			return err
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			return err
		}
		offer.Image, offer.PNG = img, data
		size := img.Bounds().Size()
		details = append(details, fmt.Sprintf("image: %dx%d", size.X, size.Y))
	}

	if len(msg.Files) > 0 {
		if !r.canUpload {
			return errors.New("uploading files is not allowed")
		}
		if len(msg.Files) > clipboardMaxPasteFiles {
			return errors.New("too many files")
		}
		var total int64
		names := make([]string, 0, len(msg.Files))
		for _, f := range msg.Files {
			name, err := cleanClipboardName(f.Name)
			if err != nil {
				return err
			}
			if f.Size < 0 || f.Dir {
				f.Size = 0
			}
			total += f.Size
			offer.Files = append(offer.Files, gordp.ClipboardFile{Name: strings.ReplaceAll(name, "/", "\\"), Size: f.Size, Dir: f.Dir, WriteTime: time.Now()})
			names = append(names, name)
		}
		if total > clipboardMaxPasteBytes {
			return fmt.Errorf("the files are larger than %d MB", clipboardMaxPasteBytes>>20)
		}
		details = append(details, "files: "+truncateRunes(strings.Join(names, ", "), 1000))

		r.offers++
		upload := &clipboardUpload{offer: offer, dir: filepath.Join(r.dir, fmt.Sprintf("%d", r.offers))}
		if err := os.MkdirAll(upload.dir, 0o700); err != nil {
			return err
		}
		for i, f := range offer.Files {
			if f.Dir {
				upload.files = append(upload.files, nil)
				continue
			}
			file, err := os.OpenFile(filepath.Join(upload.dir, fmt.Sprintf("%d", i)), os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o600)
			if err != nil {
				upload.close(true)
				return err
			}
			upload.files = append(upload.files, file)
		}
		r.upload = upload
		r.c.auditRdp(r.asset, "clipboard-paste", strings.Join(details, "\n"))
		return r.uploadProgress()
	}

	r.c.auditRdp(r.asset, "clipboard-paste", strings.Join(details, "\n"))
	r.offer(offer, nil, "")
	return nil
}

// write appends bytes of the files being pasted, in order.
func (r *rdpClipboard) write(data []byte) error {
	upload := r.upload
	if upload == nil {
		return nil
	}
	for len(data) > 0 {
		if upload.current >= len(upload.files) {
			r.abortUpload()
			return errors.New("more file data than announced")
		}
		f := upload.offer.Files[upload.current]
		n := int(min(int64(len(data)), f.Size-upload.written))
		if _, err := upload.files[upload.current].Write(data[:n]); err != nil {
			r.abortUpload()
			return err
		}
		upload.written += int64(n)
		data = data[n:]
		if err := r.uploadProgress(); err != nil {
			return err
		}
	}
	return nil
}

// uploadProgress moves past the files that are complete, and offers the
// files to the remote host once all are.
func (r *rdpClipboard) uploadProgress() error {
	upload := r.upload
	for upload.current < len(upload.files) && upload.written >= upload.offer.Files[upload.current].Size {
		upload.current++
		upload.written = 0
	}
	if upload.current < len(upload.files) {
		return nil
	}
	r.upload = nil
	for i, file := range upload.files {
		if file != nil {
			upload.offer.Files[i].Content = file
		}
	}
	r.offer(upload.offer, upload.files, upload.dir)
	return nil
}

func (r *rdpClipboard) abortUpload() {
	if r.upload != nil {
		r.upload.close(true)
		r.upload = nil
	}
}

func (u *clipboardUpload) close(remove bool) {
	for _, f := range u.files {
		if f != nil {
			_ = f.Close()
		}
	}
	if remove {
		_ = os.RemoveAll(u.dir)
	}
}

// offer puts what was pasted on the remote clipboard, dropping the files of
// the previous offer.
func (r *rdpClipboard) offer(offer *gordp.Clipboard, files []*os.File, dir string) {
	ok := r.run(func() {
		_ = r.client.SetClipboardData(offer)
		r.dropOffered()
		r.offered, r.offeredDir = files, dir
	})
	if !ok && dir != "" {
		(&clipboardUpload{files: files, dir: dir}).close(true)
	}
}

// dropOffered closes and removes the files of the current offer.
func (r *rdpClipboard) dropOffered() {
	if r.offeredDir != "" {
		(&clipboardUpload{files: r.offered, dir: r.offeredDir}).close(true)
	}
	r.offered, r.offeredDir = nil, ""
}

// close removes the files on offer once the client stopped.
func (r *rdpClipboard) close() {
	r.dropOffered()
	_ = os.RemoveAll(r.dir)
}

// cleanClipboardName checks the relative path of a pasted file.
func cleanClipboardName(name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	clean := path.Clean("/" + name)[1:]
	if clean == "" || clean != strings.Trim(name, "/") || strings.ContainsAny(clean, ":*?\"<>|") {
		return "", fmt.Errorf("invalid file name: %s", name)
	}
	for _, part := range strings.Split(clean, "/") {
		if part == "." || part == ".." || len(part) > 255 {
			return "", fmt.Errorf("invalid file name: %s", name)
		}
	}
	return clean, nil
}
