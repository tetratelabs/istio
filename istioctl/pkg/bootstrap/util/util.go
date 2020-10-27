// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package util

import (
	"fmt"
	"io"
	"os"

	"golang.org/x/crypto/ssh/terminal"
)

func RawModeStdin(in io.Reader) (io.ReadWriter, func() error, error) {
	stdin, ok := in.(io.ReadWriter)
	if !ok {
		return nil, nil, fmt.Errorf("unable to print a prompt because your terminal doesn't support it")
	}
	if stdin == os.Stdin {
		state, err := terminal.MakeRaw(0)
		if err != nil {
			return nil, nil, fmt.Errorf("unable put the terminal into raw mode: %w", err)
		}
		return stdin, func() error {
			err := terminal.Restore(0, state)
			if err != nil {
				return fmt.Errorf("unable to restore the terminal back from the raw mode: %w", err)
			}
			return nil
		}, nil
	}
	return stdin, nil, nil
}
