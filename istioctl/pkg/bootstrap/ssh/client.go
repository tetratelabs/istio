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
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

type remoteResponse struct {
	typ     uint8
	message string
}

func NewClient(stdout, stderr io.Writer) Client {
	return &client{stdout: stdout, stderr: stderr}
}

type client struct {
	stdout io.Writer
	stderr io.Writer
	client *ssh.Client
}

func (c *client) Dial(address, user string, config ssh.ClientConfig) error {
	fmt.Fprintf(c.stderr, "[SSH client] connecting to %s@%s\n", user, address)

	config.User = user

	client, err := ssh.Dial("tcp", address, &config)
	if err != nil {
		return fmt.Errorf("failed to estabslish SSH connection: %w", err)
	}
	c.client = client
	return nil
}

func (c *client) Copy(data []byte, dstPath string, perm os.FileMode, opts CopyOpts) (err error) {
	fmt.Fprintf(c.stderr, "[SSH client] copying into a remote file: %s\n", dstPath)

	defer func() {
		if err != nil {
			err = fmt.Errorf("failed to copy into a remote file %q: %w", dstPath, err)
		}
	}()
	session, err := c.newSession()
	if err != nil {
		return err
	}
	defer session.Close()

	filename := path.Base(dstPath)
	r := bytes.NewReader(data)

	wg := sync.WaitGroup{}
	wg.Add(2)
	errCh := make(chan error, 2)

	size := len(data)

	go func() {
		defer wg.Done()
		w, err := session.StdinPipe()
		if err != nil {
			errCh <- err
			return
		}
		defer w.Close()

		session.Stdout = nil // TODO(yskopets): use io.MultiWriter()
		stdout, err := session.StdoutPipe()
		if err != nil {
			errCh <- err
			return
		}

		// Set the unix file permissions (e.g., `0644`).
		//
		// If you don't read unix permissions this correlates to:
		//
		//   Owning User: READ/WRITE
		//   Owning Group: READ
		//   "Other": READ.
		//
		// We keep "OTHER"/"OWNING GROUP" to read so this seemlessly
		// works with the Istio container we start up below.
		_, err = fmt.Fprintf(w, "C0%03o %d %s\n", perm, size, filename)
		if err != nil {
			errCh <- err
			return
		}

		if err = checkRemoteResponse(stdout); err != nil {
			errCh <- err
			return
		}

		_, err = io.Copy(w, r)
		if err != nil {
			errCh <- err
			return
		}

		_, err = fmt.Fprint(w, "\x00")
		if err != nil {
			errCh <- err
			return
		}

		if err = checkRemoteResponse(stdout); err != nil {
			errCh <- err
			return
		}
	}()

	go func() {
		defer wg.Done()
		err := session.Run(fmt.Sprintf("%s -qt %s", opts.RemoteScpPath, dstPath))
		if err != nil {
			errCh <- err
			return
		}
	}()

	if waitTimeout(&wg, opts.Timeout) {
		return fmt.Errorf("timeout uploading file")
	}

	close(errCh)
	for err := range errCh {
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *client) Exec(command string) (err error) {
	fmt.Fprintf(c.stderr, "[SSH client] executing a command remotely: %s\n", command)

	defer func() {
		if err != nil {
			err = fmt.Errorf("failed to execute a remote command [%s]: %w", command, err)
		}
	}()
	session, err := c.newSession()
	if err != nil {
		return err
	}
	defer session.Close()
	return session.Run(command)
}

func (c *client) Close() error {
	fmt.Fprintf(c.stderr, "[SSH client] closing connection\n")

	if c.client == nil {
		return nil
	}
	return c.client.Close()
}

func (c *client) newSession() (*ssh.Session, error) {
	session, err := c.client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("failed to open a new SSH session: %w", err)
	}
	session.Stdout = c.stdout
	session.Stderr = c.stderr
	return session, nil
}

func parseRemoteResponse(reader io.Reader) (*remoteResponse, error) {
	buffer := make([]uint8, 1)
	if _, err := reader.Read(buffer); err != nil {
		return nil, err
	}

	typ := buffer[0]
	if typ > 0 {
		buf := bufio.NewReader(reader)
		message, err := buf.ReadString('\n')
		if err != nil {
			return nil, err
		}
		return &remoteResponse{typ, message}, nil
	}

	return &remoteResponse{typ: typ, message: ""}, nil
}

func checkRemoteResponse(r io.Reader) error {
	response, err := parseRemoteResponse(r)
	if err != nil {
		return err
	}

	if response.typ > 0 {
		return errors.New(response.message)
	}

	return nil
}

func waitTimeout(wg *sync.WaitGroup, timeout time.Duration) bool {
	c := make(chan struct{})
	go func() {
		defer close(c)
		wg.Wait()
	}()
	select {
	case <-c:
		return false // completed normally.
	case <-time.After(timeout):
		return true // timed out.
	}
}
