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
	"sync"
	"time"

	istiolog "istio.io/pkg/log"
)

var (
	log = istiolog.RegisterScope("dnsresolver", "DNS Resolver", 0)
)

const (
	defaultTTL = 30 * time.Second
)

type resolverExt interface {
	Resolver

	AllWatchedNames() []string

	Watches(dnsName string) bool

	AllWatchers() []Referer

	Watchers(dnsName string) []Referer
}

func NewFixedResolver(addresses map[string][]net.IP) Resolver {
	return NewResolver(ResolverOpts{Client: newStaticClient(addresses)})
}

func NewResolver(opts ResolverOpts) Resolver {
	if opts.Client == nil {
		opts.Client = newDefaultClient()
	}
	return &resolver{
		opts:     opts,
		updateCh: make(chan dnsUpdate, 10),
		referers: make(map[Referer]NameSet),
		watches:  make(map[string]*dnsWatch),
	}
}

type dnsUpdate struct {
	dnsName   string
	addresses []net.IP
}

type dnsWatch struct {
	referers RefererSet
	answers  []string
	stopCh   chan struct{}
}

type resolver struct {
	opts     ResolverOpts
	once     sync.Once
	updateCh chan dnsUpdate
	mu       sync.RWMutex
	referers map[Referer]NameSet
	watches  map[string]*dnsWatch
	handlers []UpdateHandler
}

func (r *resolver) Watch(referer Referer, dnsNames []string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	names := NewNameSet(dnsNames...)
	added, deleted := names.Diff(r.referers[referer])
	for dnsName := range added {
		r.watch(dnsName, referer)
	}
	for dnsName := range deleted {
		r.cancel(dnsName, referer)
	}
	r.referers[referer] = names
}

func (r *resolver) watch(dnsName string, referer Referer) {
	watch, exists := r.watches[dnsName]
	if exists {
		watch.referers.Add(referer)
		return
	}
	r.once.Do(func() {
		go r.waitUpdates()
	})
	watch = &dnsWatch{
		referers: NewRefererSet(referer),
		stopCh:   make(chan struct{}),
	}
	go watchDNSName(dnsName, watch.stopCh, r.opts.Client, r.updateCh)
	r.watches[dnsName] = watch
}

func (r *resolver) Cancel(referer Referer) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for dnsName := range r.referers[referer] {
		r.cancel(dnsName, referer)
	}
	delete(r.referers, referer)
}

func (r *resolver) cancel(dnsName string, referer Referer) {
	watch, exists := r.watches[dnsName]
	if !exists {
		return
	}
	watch.referers.Remove(referer)
	if watch.referers.Empty() {
		close(watch.stopCh)
		delete(r.watches, dnsName)
	}
}

func (r *resolver) LookupIP(dnsName string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	watch, exists := r.watches[dnsName]
	if !exists {
		return nil
	}
	return watch.answers
}

func (r *resolver) AddUpdateHandler(handler UpdateHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.handlers = append(r.handlers, handler)
}

func (r *resolver) waitUpdates() {
	for update := range r.updateCh {
		r.mu.Lock()

		watch, exists := r.watches[update.dnsName]
		if !exists {
			r.mu.Unlock()
			continue
		}
		prev := watch.answers
		next := make([]string, len(update.addresses))
		for i, ip := range update.addresses {
			next[i] = ip.String()
		}
		watch.answers = next
		r.mu.Unlock()

		if NewNameSet(prev...).Equal(NewNameSet(next...)) {
			continue
		}

		r.mu.RLock()
		log.Debugf("Notifying %d handlers that DNS name %q now resolves into a different set of IP addresses: %v",
			len(r.handlers), update.dnsName, next)
		for _, handler := range r.handlers {
			handler(update.dnsName)
		}
		r.mu.RUnlock()
	}
}

func (r *resolver) AllWatchedNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.watches) == 0 {
		return nil
	}
	names := make([]string, 0, len(r.watches))
	for name := range r.watches {
		names = append(names, name)
	}
	return names
}

func (r *resolver) Watches(dnsName string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, exists := r.watches[dnsName]
	return exists
}

func (r *resolver) AllWatchers() []Referer {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.referers) == 0 {
		return nil
	}
	watchers := make([]Referer, 0, len(r.referers))
	for referer := range r.referers {
		watchers = append(watchers, referer)
	}
	return watchers
}

func (r *resolver) Watchers(dnsName string) []Referer {
	r.mu.RLock()
	defer r.mu.RUnlock()

	watch, exists := r.watches[dnsName]
	if !exists {
		return nil
	}
	watchers := make([]Referer, 0, len(watch.referers))
	for referer := range watch.referers {
		watchers = append(watchers, referer)
	}
	return watchers
}

func watchDNSName(dnsName string, stopCh <-chan struct{}, client Client, updateCh chan<- dnsUpdate) {
	effectiveTTL := defaultTTL
	deadline := time.Now().Add(effectiveTTL)
	delayTillRefresh := 0 * time.Nanosecond
	for {
		select {
		case <-stopCh:
			return
		case <-time.After(delayTillRefresh):
		}

		log.Debugf("Resolving DNS name %q", dnsName)
		addresses, ttl, err := client.LookupIP(dnsName)
		if err != nil {
			delayTillRefresh /= 2
			if delayTillRefresh < effectiveTTL/32 {
				delayTillRefresh = effectiveTTL / 32
			}
			log.Warnf("Failed to resolve DNS name %q, will retry in %s: %v", dnsName, delayTillRefresh, err)

			if time.Now().After(deadline) {
				log.Warnf("Clearing cache for DNS name %q since deadline has been exceeded while refresh attempts keep failing", dnsName)
				updateCh <- dnsUpdate{dnsName: dnsName, addresses: nil}
			}
			continue
		}
		if ttl > 0 {
			effectiveTTL = ttl
		} else {
			effectiveTTL = defaultTTL
		}
		deadline = time.Now().Add(effectiveTTL)
		delayTillRefresh = effectiveTTL / 2
		log.Debugf("Resolved DNS name %q into %v, effective TTL == %s, will repeat DNS resolution in %s",
			dnsName, addresses, effectiveTTL, delayTillRefresh)

		updateCh <- dnsUpdate{dnsName: dnsName, addresses: addresses}
	}
}
