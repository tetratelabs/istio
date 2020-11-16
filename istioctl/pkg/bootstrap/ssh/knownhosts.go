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

package ssh

import (
	"fmt"
	"io"
	"net"
	"strings"

	"github.com/hashicorp/go-multierror"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/terminal"

	"istio.io/istio/istioctl/pkg/bootstrap/util"
)

func HostKeyCallbackChain(callbacks ...ssh.HostKeyCallback) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) (err error) {
		for _, callback := range callbacks {
			err = callback(hostname, remote, key)
			if err == nil {
				return
			}
		}
		return
	}
}

func HostKeyPrompt(stdin io.Reader, stderr io.Writer) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) (errs error) {
		call := func(fn func() error) {
			if fn == nil {
				return
			}
			err := fn()
			if err != nil {
				errs = multierror.Append(errs, err)
			}
		}
		rawModeStdin, restoreStdin, err := util.RawModeStdin(stdin)
		if err != nil {
			return err
		}
		defer call(restoreStdin)

		term := terminal.NewTerminal(rawModeStdin, "")
		fmt.Fprintf(stderr, "The authenticity of host '%s (%s)' can't be established.\r\n", host(hostname), host(remote.String()))
		fmt.Fprintf(stderr, "%s key fingerprint is %s.\r\n", strings.ToUpper(key.Type()), ssh.FingerprintSHA256(key))
		term.SetPrompt("Are you sure you want to continue connecting (yes/no)? ")
		answer, err := term.ReadLine()
		if err != nil {
			return err
		}
		for {
			switch answer {
			case "yes":
				return nil
			case "no":
				return fmt.Errorf("host key verification failed") // error message similar to the SSH CLI
			default:
				term.SetPrompt("Please type 'yes' or 'no': ")
				answer, err = term.ReadLine()
				if err != nil {
					return err
				}
			}
		}
	}
}

func host(address string) string {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return address
	}
	return host
}
