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

package sshgateway

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/casvisor/casvisor/object"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// serveSftp answers the client's SFTP requests itself and carries them out with an
// SFTP client on the asset, so every read, write and change can be checked against
// the Upload and Download permissions and written to the audit log.
func (s *sessionChannel) serveSftp(req *ssh.Request) {
	g := s.g
	if g.getAsset() == nil {
		s.fail(req, "no asset is selected, connect with \"sftp -P <port> <asset>@<host>\"")
		return
	}
	if !g.can(object.ActionUpload) && !g.can(object.ActionDownload) {
		s.fail(req, fmt.Sprintf("you have no permission to transfer files on the asset: %s", g.getAsset().Name))
		return
	}

	client, err := g.getClient()
	if err != nil {
		s.fail(req, err.Error())
		return
	}

	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		s.fail(req, err.Error())
		return
	}
	defer sftpClient.Close()

	startDirectory, err := sftpClient.Getwd()
	if err != nil {
		startDirectory = "/"
	}

	_ = req.Reply(true, nil)

	h := &sftpHandler{g: g, client: sftpClient}
	handlers := sftp.Handlers{FileGet: h, FilePut: h, FileCmd: h, FileList: h}
	server := sftp.NewRequestServer(s.down, handlers, sftp.WithStartDirectory(startDirectory))
	_ = server.Serve()
	// the exit status has to go before Close, which closes the channel
	exit(s.down, 0)
	_ = server.Close()
}

type sftpHandler struct {
	g      *gatewayConn
	client *sftp.Client
}

func (h *sftpHandler) Fileread(r *sftp.Request) (io.ReaderAt, error) {
	if !h.g.can(object.ActionDownload) {
		return nil, sftp.ErrSSHFxPermissionDenied
	}

	file, err := h.client.Open(r.Filepath)
	if err != nil {
		return nil, err
	}
	h.g.audit("sftp-download", r.Filepath)
	return file, nil
}

func (h *sftpHandler) Filewrite(r *sftp.Request) (io.WriterAt, error) {
	if !h.g.can(object.ActionUpload) {
		return nil, sftp.ErrSSHFxPermissionDenied
	}

	flags := r.Pflags()
	osFlags := os.O_WRONLY
	if flags.Read {
		osFlags = os.O_RDWR
	}
	if flags.Append {
		osFlags |= os.O_APPEND
	}
	if flags.Creat {
		osFlags |= os.O_CREATE
	}
	if flags.Trunc {
		osFlags |= os.O_TRUNC
	}
	if flags.Excl {
		osFlags |= os.O_EXCL
	}

	file, err := h.client.OpenFile(r.Filepath, osFlags)
	if err != nil {
		return nil, err
	}
	h.g.audit("sftp-upload", r.Filepath)
	return file, nil
}

func (h *sftpHandler) Filecmd(r *sftp.Request) error {
	if !h.g.can(object.ActionUpload) {
		return sftp.ErrSSHFxPermissionDenied
	}

	var err error
	detail := r.Filepath
	switch r.Method {
	case "Setstat":
		err = h.setstat(r)
	case "Rename":
		err = h.client.Rename(r.Filepath, r.Target)
		detail = fmt.Sprintf("%s -> %s", r.Filepath, r.Target)
	case "Rmdir":
		err = h.client.RemoveDirectory(r.Filepath)
	case "Remove":
		err = h.client.Remove(r.Filepath)
	case "Mkdir":
		err = h.client.Mkdir(r.Filepath)
	case "Link":
		err = h.client.Link(r.Filepath, r.Target)
		detail = fmt.Sprintf("%s -> %s", r.Target, r.Filepath)
	case "Symlink":
		// for symlinks, Filepath is where the link points to and Target is the link
		err = h.client.Symlink(r.Filepath, r.Target)
		detail = fmt.Sprintf("%s -> %s", r.Target, r.Filepath)
	default:
		return sftp.ErrSSHFxOpUnsupported
	}
	if err != nil {
		return err
	}

	if r.Method != "Setstat" {
		h.g.audit("sftp-"+strings.ToLower(r.Method), detail)
	}
	return nil
}

func (h *sftpHandler) setstat(r *sftp.Request) error {
	flags := r.AttrFlags()
	attrs := r.Attributes()
	if flags.Size {
		err := h.client.Truncate(r.Filepath, int64(attrs.Size))
		if err != nil {
			return err
		}
	}
	if flags.Permissions {
		err := h.client.Chmod(r.Filepath, attrs.FileMode())
		if err != nil {
			return err
		}
	}
	if flags.UidGid {
		err := h.client.Chown(r.Filepath, int(attrs.UID), int(attrs.GID))
		if err != nil {
			return err
		}
	}
	if flags.Acmodtime {
		err := h.client.Chtimes(r.Filepath, time.Unix(int64(attrs.Atime), 0), time.Unix(int64(attrs.Mtime), 0))
		if err != nil {
			return err
		}
	}
	return nil
}

func (h *sftpHandler) PosixRename(r *sftp.Request) error {
	if !h.g.can(object.ActionUpload) {
		return sftp.ErrSSHFxPermissionDenied
	}

	err := h.client.PosixRename(r.Filepath, r.Target)
	if err != nil {
		return err
	}
	h.g.audit("sftp-rename", fmt.Sprintf("%s -> %s", r.Filepath, r.Target))
	return nil
}

func (h *sftpHandler) StatVFS(r *sftp.Request) (*sftp.StatVFS, error) {
	return h.client.StatVFS(r.Filepath)
}

func (h *sftpHandler) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	switch r.Method {
	case "List":
		entries, err := h.client.ReadDir(r.Filepath)
		if err != nil {
			return nil, err
		}
		return newListerAt(entries), nil
	case "Stat":
		info, err := h.client.Stat(r.Filepath)
		if err != nil {
			return nil, err
		}
		return newListerAt([]os.FileInfo{info}), nil
	default:
		return nil, sftp.ErrSSHFxOpUnsupported
	}
}

func (h *sftpHandler) Lstat(r *sftp.Request) (sftp.ListerAt, error) {
	info, err := h.client.Lstat(r.Filepath)
	if err != nil {
		return nil, err
	}
	return newListerAt([]os.FileInfo{info}), nil
}

func (h *sftpHandler) Readlink(path string) (string, error) {
	return h.client.ReadLink(path)
}

func (h *sftpHandler) RealPath(path string) (string, error) {
	return h.client.RealPath(path)
}

type listerAt []os.FileInfo

func newListerAt(entries []os.FileInfo) listerAt {
	res := make(listerAt, 0, len(entries))
	for _, entry := range entries {
		res = append(res, fileInfo{entry})
	}
	return res
}

func (l listerAt) ListAt(entries []os.FileInfo, offset int64) (int, error) {
	if offset >= int64(len(l)) {
		return 0, io.EOF
	}

	n := copy(entries, l[offset:])
	if n < len(entries) {
		return n, io.EOF
	}
	return n, nil
}

// fileInfo passes the owner of a file on the asset on to the client.
type fileInfo struct {
	os.FileInfo
}

func (info fileInfo) Uid() uint32 {
	if stat, ok := info.Sys().(*sftp.FileStat); ok {
		return stat.UID
	}
	return 0
}

func (info fileInfo) Gid() uint32 {
	if stat, ok := info.Sys().(*sftp.FileStat); ok {
		return stat.GID
	}
	return 0
}
