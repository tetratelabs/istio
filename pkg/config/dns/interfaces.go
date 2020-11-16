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

type Referer struct {
	APIGroup  string
	Kind      string
	Namespace string
	Name      string
}

type RefererSet map[Referer]bool

type UpdateHandler func(dnsName string)

type Lookup interface {
	LookupIP(dnsName string) []string
}

type Resolver interface {
	Lookup

	Watch(referer Referer, dnsNames []string)

	Cancel(referer Referer)

	AddUpdateHandler(UpdateHandler)
}

type ResolverOpts struct {
	Client
}

type Client interface {
	LookupIP(name string) (addresses []net.IP, ttl time.Duration, err error)
}

func NewRefererSet(referers ...Referer) RefererSet {
	set := make(RefererSet)
	set.AddAll(referers...)
	return set
}

func (s RefererSet) Empty() bool {
	return len(s) == 0
}

func (s RefererSet) Add(referer Referer) RefererSet {
	s[referer] = true
	return s
}

func (s RefererSet) AddAll(referers ...Referer) RefererSet {
	for _, referer := range referers {
		s.Add(referer)
	}
	return s
}

func (s RefererSet) Remove(referer Referer) RefererSet {
	delete(s, referer)
	return s
}

type noopLookup struct{}

func (l noopLookup) LookupIP(dnsName string) []string {
	return nil
}

var noLookup noopLookup

func LookupOrNoop(lookup Lookup) Lookup {
	if lookup != nil {
		return lookup
	}
	return noLookup
}
