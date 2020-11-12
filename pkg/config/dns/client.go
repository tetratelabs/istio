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

package dns

import (
	"net"
	"time"
)

func newDefaultClient() Client {
	return newStdClient()
}

type stdClient struct{}

func (c *stdClient) LookupIP(name string) (addresses []net.IP, ttl time.Duration, err error) {
	addrs, err := net.LookupIP(name)
	if err != nil {
		return nil, 0, err
	}
	return addrs, 0, nil
}

func newStdClient() Client {
	return new(stdClient)
}

type staticClient map[string][]net.IP

func (c staticClient) LookupIP(name string) (addresses []net.IP, ttl time.Duration, err error) {
	return c[name], 0, nil
}

func newStaticClient(addresses map[string][]net.IP) Client {
	return staticClient(addresses)
}
