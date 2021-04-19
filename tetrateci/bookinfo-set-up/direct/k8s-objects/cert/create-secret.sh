kubectl -n bookinfo-direct create secret tls bookinfo-direct-certs \
    --key bookinfo-direct.key \
    --cert bookinfo-direct.crt


kubectl -n bookinfo-direct create secret generic bookinfo-direct-ca-certs \
	--from-file=bookinfo-direct-ca.crt=bookinfo-direct-ca.crt
