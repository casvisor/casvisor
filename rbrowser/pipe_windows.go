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

package rbrowser

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

func setChildPipes(cmd *exec.Cmd, childR *os.File, childW *os.File, parentR *os.File, parentW *os.File) error {
	for _, f := range []*os.File{parentR, parentW} {
		err := syscall.SetHandleInformation(syscall.Handle(f.Fd()), syscall.HANDLE_FLAG_INHERIT, 0)
		if err != nil {
			return err
		}
	}

	hr := syscall.Handle(childR.Fd())
	hw := syscall.Handle(childW.Fd())
	cmd.SysProcAttr = &syscall.SysProcAttr{
		AdditionalInheritedHandles: []syscall.Handle{hr, hw},
		HideWindow:                 true,
	}
	cmd.Args = append(cmd.Args, "--remote-debugging-pipe", "--remote-debugging-io-pipes="+strconv.FormatUint(uint64(hr), 10)+","+strconv.FormatUint(uint64(hw), 10))
	return nil
}
