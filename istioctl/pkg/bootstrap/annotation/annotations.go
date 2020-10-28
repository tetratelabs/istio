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

package annotation

const (
	// IP address or DNS name of the machine represented by this WorkloadEntry to use
	// instead of WorkloadEntry.Address for SSH connections from `istioctl x sidecar-bootstrap`.
	//
	// This setting is intended for those scenarios where `istioctl x sidecar-bootstrap`
	// will be run on a machine without direct connectivity to the WorkloadEntry.Address.
	// E.g., one might set WorkloadEntry.Address to the "internal IP" of a VM
	// and set value of this annotation to the "external IP" of that VM.
	//
	// By default, value of WorkloadEntry.Address is assumed.
	SshHost = "sidecar-bootstrap.istioctl.istio.io/ssh-host"

	// Port of the SSH server on the machine represented by this WorkloadEntry to use
	// for SSH connections from `istioctl x sidecar-bootstrap`.
	//
	// By default, `22` is assumed.
	SshPort = "sidecar-bootstrap.istioctl.istio.io/ssh-port"

	// User on the machine represented by this WorkloadEntry to use for SSH connections
	// from `istioctl x sidecar-bootstrap`.
	//
	// Make sure that user has enough permissions to create the config dir and
	// to run Docker container without `sudo`.
	//
	// By default, a user running `istioctl x sidecar-bootstrap` is assumed.
	SshUser = "sidecar-bootstrap.istioctl.istio.io/ssh-user"

	// Path to the `scp` binary on the machine represented by this WorkloadEntry to use
	// in SSH connections from `istioctl x sidecar-bootstrap`.
	//
	// By default, `/usr/bin/scp` is assumed.
	ScpPath = "sidecar-bootstrap.istioctl.istio.io/scp-path"

	// Directory on the machine represented by this WorkloadEntry where `istioctl x sidecar-bootstrap`
	// should copy bootstrap bundle to.
	//
	// By default, `/tmp/istio-proxy` is assumed (the most reliable default value for out-of-the-box experience).
	DestinationDir = "sidecar-bootstrap.istioctl.istio.io/destination-dir"

	// Hub with Istio Proxy images that the the machine represented by this WorkloadEntry
	// should pull from instead of a mesh-wide hub.
	//
	// By default, mesh-wide hub is assumed.
	ProxyImageHub = "sidecar-bootstrap.istioctl.istio.io/proxy-image-hub"
)
