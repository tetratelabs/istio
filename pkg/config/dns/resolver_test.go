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
	"testing"
	"time"

	. "github.com/onsi/gomega"
)

var (
	meshNetworksRef = Referer{
		APIGroup: "istio.mesh",
		Kind:     "MeshNetworks",
	}
)

func Test_Resolver_Watch_Cancel_By_One_Watcher(t *testing.T) {
	g := NewGomegaWithT(t)

	resolver := NewResolver(ResolverOpts{}).(resolverExt)

	g.Expect(resolver).NotTo(BeNil())
	g.Expect(resolver.AllWatchedNames()).To(BeNil())
	g.Expect(resolver.AllWatchers()).To(BeNil())

	resolver.Watch(meshNetworksRef, []string{"x.y.elb.amazonaws.com"})

	g.Expect(resolver.AllWatchedNames()).To(ConsistOf("x.y.elb.amazonaws.com"))
	g.Expect(resolver.AllWatchers()).To(ConsistOf(meshNetworksRef))
	g.Expect(resolver.Watchers("x.y.elb.amazonaws.com")).To(ConsistOf(meshNetworksRef))

	resolver.Cancel(meshNetworksRef)

	g.Expect(resolver.AllWatchedNames()).To(BeNil())
	g.Expect(resolver.AllWatchers()).To(BeNil())
}

func Test_Resolver_Update_By_One_Watcher(t *testing.T) {
	g := NewGomegaWithT(t)

	resolver := NewResolver(ResolverOpts{}).(resolverExt)

	resolver.Watch(meshNetworksRef, []string{"x.y.elb.amazonaws.com"})

	resolver.Watch(meshNetworksRef, []string{"x.y.elb.amazonaws.com", "a.b.elb.amazonaws.com"})

	g.Expect(resolver.AllWatchedNames()).To(ConsistOf("x.y.elb.amazonaws.com", "a.b.elb.amazonaws.com"))
	g.Expect(resolver.AllWatchers()).To(ConsistOf(meshNetworksRef))
	g.Expect(resolver.Watchers("x.y.elb.amazonaws.com")).To(ConsistOf(meshNetworksRef))
	g.Expect(resolver.Watchers("a.b.elb.amazonaws.com")).To(ConsistOf(meshNetworksRef))

	resolver.Watch(meshNetworksRef, []string{"a.b.elb.amazonaws.com"})

	g.Expect(resolver.AllWatchedNames()).To(ConsistOf("a.b.elb.amazonaws.com"))
	g.Expect(resolver.AllWatchers()).To(ConsistOf(meshNetworksRef))
	g.Expect(resolver.Watchers("a.b.elb.amazonaws.com")).To(ConsistOf(meshNetworksRef))

	resolver.Watch(meshNetworksRef, []string{})

	g.Expect(resolver.AllWatchedNames()).To(BeNil())
	g.Expect(resolver.AllWatchers()).To(ConsistOf(meshNetworksRef))

	resolver.Cancel(meshNetworksRef)

	g.Expect(resolver.AllWatchedNames()).To(BeNil())
	g.Expect(resolver.AllWatchers()).To(BeNil())
}

func Test_Resolver_Watch_Cancel_By_Two_Watchers(t *testing.T) {
	g := NewGomegaWithT(t)

	resolver := NewResolver(ResolverOpts{}).(resolverExt)

	resolver.Watch(meshNetworksRef, []string{"x.y.elb.amazonaws.com"})

	otherWatcherRef := Referer{
		APIGroup: "other APIGroup",
		Kind:     "other Kind",
	}

	resolver.Watch(otherWatcherRef, []string{"x.y.elb.amazonaws.com"})

	g.Expect(resolver.AllWatchedNames()).To(ConsistOf("x.y.elb.amazonaws.com"))
	g.Expect(resolver.AllWatchers()).To(ConsistOf(meshNetworksRef, otherWatcherRef))
	g.Expect(resolver.Watchers("x.y.elb.amazonaws.com")).To(ConsistOf(meshNetworksRef, otherWatcherRef))

	resolver.Cancel(otherWatcherRef)

	g.Expect(resolver.AllWatchedNames()).To(ConsistOf("x.y.elb.amazonaws.com"))
	g.Expect(resolver.AllWatchers()).To(ConsistOf(meshNetworksRef))
	g.Expect(resolver.Watchers("x.y.elb.amazonaws.com")).To(ConsistOf(meshNetworksRef))

	resolver.Cancel(meshNetworksRef)

	g.Expect(resolver.AllWatchedNames()).To(BeNil())
	g.Expect(resolver.AllWatchers()).To(BeNil())
}

func Test_Resolver_Updates(t *testing.T) {
	g := NewGomegaWithT(t)

	responsesCh := make(chan FakeResponse)

	resolver := NewResolver(ResolverOpts{
		Client: ClientFuncs{
			LookupIPFunc: func(name string) (addresses []net.IP, ttl time.Duration, err error) {
				response := <-responsesCh
				return response.addresses, response.ttl, response.err
			},
		},
	}).(resolverExt)

	actual := resolver.LookupIP("x.y.elb.amazonaws.com")
	g.Expect(actual).To(BeNil())

	resolver.Watch(meshNetworksRef, []string{"x.y.elb.amazonaws.com"})

	actual = resolver.LookupIP("x.y.elb.amazonaws.com")
	g.Expect(actual).To(BeNil())

	responsesCh <- FakeResponse{
		addresses: []net.IP{net.ParseIP("1.2.3.4"), net.ParseIP("5.6.7.8")},
	}

	g.Eventually(func() []string {
		return resolver.LookupIP("x.y.elb.amazonaws.com")
	}).Should(ConsistOf("1.2.3.4", "5.6.7.8"))

	responsesCh <- FakeResponse{
		addresses: []net.IP{net.ParseIP("9.0.1.2")},
	}

	g.Eventually(func() []string {
		return resolver.LookupIP("x.y.elb.amazonaws.com")
	}).Should(ConsistOf("9.0.1.2"))
}

type ClientFuncs struct {
	LookupIPFunc func(name string) (addresses []net.IP, ttl time.Duration, err error)
}

func (f ClientFuncs) LookupIP(name string) (addresses []net.IP, ttl time.Duration, err error) {
	return f.LookupIPFunc(name)
}

type FakeResponse struct {
	addresses []net.IP
	ttl       time.Duration
	err       error
}
