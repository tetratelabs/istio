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
	"sort"
)

type NameSet map[string]bool

func NewNameSet(names ...string) NameSet {
	set := make(NameSet)
	set.AddAll(names...)
	return set
}

func (s NameSet) Add(name string) NameSet {
	s[name] = true
	return s
}

func (s NameSet) AddAll(names ...string) NameSet {
	for _, name := range names {
		s.Add(name)
	}
	return s
}

func (s NameSet) Contains(name string) bool {
	return s[name]
}

func (s NameSet) List() []string {
	if len(s) == 0 {
		return nil
	}
	list := make([]string, 0, len(s))
	for name := range s {
		list = append(list, name)
	}
	sort.Strings(list)
	return list
}

func (s NameSet) Diff(old NameSet) (added NameSet, deleted NameSet) {
	added, deleted = make(NameSet), make(NameSet)

	for name := range s {
		if !old.Contains(name) {
			added.Add(name)
		}
	}
	for name := range old {
		if !s.Contains(name) {
			deleted.Add(name)
		}
	}
	return
}

func (s NameSet) Equal(other NameSet) bool {
	if len(s) != len(other) {
		return false
	}
	for name := range s {
		if !other.Contains(name) {
			return false
		}
	}
	return true
}
