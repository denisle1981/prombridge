# Build and deploy the Go receiver

## 1. Build with a unique tag

Run from the project root:

```bash
docker build --no-cache \
  -f Dockerfile.receiver \
  -t prom-udp-receiver:0.2.1 .
```

The final image is expected to be only a few MB because it contains one stripped static Go binary in a `scratch` image.

## 2. Verify before pushing

```bash
./verify-receiver-image.sh prom-udp-receiver:0.2.1
```

Expected entrypoint:

```text
["/prom-udp-receiver"]
```

The help output must contain these flags:

```text
--udp-listen
--http-listen
--reassembly-timeout
--max-compressed-bytes
--max-uncompressed-bytes
--max-packet-bytes
```

If output mentions `prom_udp_receiver.py`, you are still using the old Python image.

## 3. Transfer to an air-gapped registry

Connected build host:

```bash
docker save prom-udp-receiver:0.2.1 | gzip > prom-udp-receiver-0.2.1.tar.gz
```

Air-gapped host:

```bash
gunzip -c prom-udp-receiver-0.2.1.tar.gz | docker load
docker tag prom-udp-receiver:0.2.1 REGISTRY/monitoring/prom-udp-receiver:0.2.1
docker push REGISTRY/monitoring/prom-udp-receiver:0.2.1
```

## 4. Helm values

```yaml
image:
  repository: REGISTRY/monitoring/prom-udp-receiver
  tag: "0.2.1"
  pullPolicy: Always
```

Using a new tag is important. Reusing `0.2.0` with `IfNotPresent` can leave the old Python image cached on an RKE2 node.

## 5. Upgrade

```bash
helm upgrade --install prom-udp-relay \
  ./helm/prometheus-udp-relay \
  -n prometheus \
  -f values.yaml

kubectl -n prometheus rollout status deployment/prom-udp-relay-prometheus-udp-relay
```

Check the actual image and command:

```bash
kubectl -n prometheus get pod -l app.kubernetes.io/instance=prom-udp-relay \
  -o jsonpath='{range .items[*]}{.metadata.name}{"  "}{.spec.containers[0].image}{"\n"}{end}'

kubectl -n prometheus logs deployment/prom-udp-relay-prometheus-udp-relay
```
