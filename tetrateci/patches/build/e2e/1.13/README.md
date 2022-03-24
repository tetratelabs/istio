# Patches to Istio 1.11

## 0001-Allow-turning-off-ALPN-in-echo-server-35447.patch

### Why do we need it?

Integration tests of `Istio 1.11` were relying on certain behaviour
of the HTTPS server from the Go standard library.

In `Go 1.17`, behaviour of the HTTPS server has changed and tests started failing.

In `Istio 1.12` they changed integration tests in order to be able to upgrade to `Go 1.17`.

See https://github.com/istio/istio/pull/35447
