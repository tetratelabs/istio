kubectl -n bookinfo create secret tls bookinfo-certs \
    --key bookinfo.key \
    --cert bookinfo.crt


kubectl -n bookinfo create secret generic bookinfo-ca-certs \
	--from-file=bookinfo-ca.crt=bookinfo-ca.crt
