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

package fake

import (
	"fmt"
	"io"
	"os"

	"golang.org/x/crypto/ssh"

	bootstrapSsh "istio.io/istio/istioctl/pkg/bootstrap/ssh"
)

func NewClient(_, stderr io.Writer) bootstrapSsh.Client {
	return &client{stderr: stderr}
}

type client struct {
	stderr io.Writer
}

func (c *client) Dial(address, username string, _ ssh.ClientConfig) error {
	fmt.Fprintf(c.stderr, "\n[SSH client] going to connect to %s@%s\n", username, address)
	return nil
}

func (c *client) Copy(data []byte, dstPath string, _ os.FileMode, _ bootstrapSsh.CopyOpts) error {
	fmt.Fprintf(c.stderr, "\n[SSH client] going to copy into a remote file: %s\n%s\n", dstPath, string(data))
	return nil
}

func (c *client) Exec(command string) error {
	fmt.Fprintf(c.stderr, "\n[SSH client] going to execute a command remotely: %s\n", command)
	return nil
}

func (c *client) Close() error {
	fmt.Fprintf(c.stderr, "\n[SSH client] going to close connection\n")
	return nil
}
