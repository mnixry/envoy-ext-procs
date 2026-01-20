# Envoy External Processors

This repository contains gRPC external processors for Envoy's ext_proc filter.
It provides production-ready binaries with TLS support, health checks, and configuration via CLI flags or environment variables.

## Processors

- `accesslog`: Emits Caddy-style JSON access logs for each request/response.
  Sensitive headers are redacted by default.
- `edgeone-real-ip`: Validates Tencent EdgeOne CDN requests and sets
  `x-forwarded-for` and `x-real-ip` based on `eo-connecting-ip`. It also sets
  `x-forwarded-from-edgeone` to `yes`, `no`, or `unknown`.

## Build

```bash
make build
```

Artifacts are written to `bin/`:

- `bin/accesslog`
- `bin/edgeone-real-ip`

Docker build:

```bash
docker build -t envoy-ext-procs:local .
```

## Run

Both processors require TLS certificates on disk. The gRPC server expects a
directory containing `server.crt` and `server.key`.

Access log:

```bash
./bin/accesslog \
  --grpc-cert-path=/etc/ext-proc/certs \
  --grpc-ca-file=/etc/ext-proc/ca.crt \
  --health-dial-server-name=ext-proc-accesslog.envoygateway
```

EdgeOne real IP:

```bash
EDGEONE_SECRET_ID=... \
EDGEONE_SECRET_KEY=... \
./bin/edgeone-real-ip \
  --grpc-cert-path=/etc/ext-proc/certs \
  --grpc-ca-file=/etc/ext-proc/ca.crt \
  --health-dial-server-name=ext-proc-real-ip.envoygateway
```

## Configuration

Common flags and environment variables:

- `--grpc-port` / `GRPC_PORT` (default: `9002`)
- `--grpc-cert-path` / `GRPC_CERT_PATH` (directory with `server.crt` and `server.key`)
- `--grpc-ca-file` / `GRPC_CA_FILE` (CA bundle used by health checks)
- `--health-port` / `HEALTH_PORT` (default: `8080`)
- `--health-dial-server-name` / `HEALTH_DIAL_SERVER_NAME`
- `--log-level` / `LOG_LEVEL`
- `--log-output` / `LOG_OUTPUT` (`stdout`, `stderr`, or file path)
- `--log-format` / `LOG_FORMAT` (`json` or `console`)
- `--log-max-size` / `LOG_MAX_SIZE`
- `--log-max-age` / `LOG_MAX_AGE`
- `--log-max-backups` / `LOG_MAX_BACKUPS`
- `--log-compress` / `LOG_COMPRESS`

Access log specific:

- `--exclude-headers` / `EXCLUDE_HEADERS` (comma-separated list)
  - Default redactions: `cookie`, `set-cookie`, `authorization`,
    `proxy-authorization`

EdgeOne specific:

- `--edgeone-secret-id` / `EDGEONE_SECRET_ID`
- `--edgeone-secret-key` / `EDGEONE_SECRET_KEY`
- `--edgeone-api-endpoint` / `EDGEONE_API_ENDPOINT`
- `--edgeone-region` / `EDGEONE_REGION`
- `--edgeone-cache-size` / `EDGEONE_CACHE_SIZE`
- `--edgeone-cache-ttl` / `EDGEONE_CACHE_TTL`
- `--edgeone-timeout` / `EDGEONE_TIMEOUT`

## Envoy Gateway Integration Notes

- The ext_proc server is TLS-only. `BackendTLSPolicy` should validate the
  service certificate with the same CA used by the processors.
- The health check endpoint (`/healthz`) performs a TLS gRPC health call. It
  uses `--grpc-ca-file` and `--health-dial-server-name`.
- `edgeone-real-ip` needs `source.address` attributes. Ensure the
  `EnvoyExtensionPolicy` processing mode requests them.

## Kubernetes Example

The following examples are based on the Helm chart and values used in
`merak-manifest/infra/envoy-gateway/external-processing`.

### Example Helm Values

```yaml
image: ghcr.io/mnixry/envoy-ext-procs:main
processors:
  - name: ext-proc-real-ip
    binary: edgeone-real-ip
    envFromSecrets:
      - tencent-eo-secret
  - name: ext-proc-accesslog
    binary: accesslog
    podLabels:
      k8s.buptmerak.cn/service: ext-proc-accesslog
      k8s.buptmerak.cn/logging: "false"
```

### Example Raw Manifests (single processor)

This example deploys `edgeone-real-ip`. Duplicate the Deployment and Service
for `accesslog` and update the binary and secrets.

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: envoy-gateway-system
---
apiVersion: v1
kind: Secret
metadata:
  name: tencent-eo-secret
  namespace: envoy-gateway-system
type: Opaque
stringData:
  EDGEONE_SECRET_ID: "${EDGEONE_SECRET_ID}"
  EDGEONE_SECRET_KEY: "${EDGEONE_SECRET_KEY}"
---
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: ext-proc-ca-issuer
  namespace: envoy-gateway-system
spec:
  selfSigned: {}
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: ext-proc-ca-cert
  namespace: envoy-gateway-system
spec:
  isCA: true
  commonName: "Envoy External Processing CA"
  issuerRef:
    kind: Issuer
    name: ext-proc-ca-issuer
  secretName: ext-proc-ca-cert-secret
---
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: ext-proc-leaf-issuer
  namespace: envoy-gateway-system
spec:
  ca:
    secretName: ext-proc-ca-cert-secret
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: ext-proc-real-ip-cert
  namespace: envoy-gateway-system
spec:
  secretName: ext-proc-real-ip-cert-secret
  commonName: ext-proc-real-ip
  dnsNames:
    - ext-proc-real-ip.envoygateway
  issuerRef:
    kind: Issuer
    name: ext-proc-leaf-issuer
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ext-proc-real-ip
  namespace: envoy-gateway-system
spec:
  replicas: 1
  selector:
    matchLabels:
      app: ext-proc-real-ip
  template:
    metadata:
      labels:
        app: ext-proc-real-ip
    spec:
      containers:
        - name: ext-proc
          image: ghcr.io/mnixry/envoy-ext-procs:main
          args:
            - /usr/local/bin/edgeone-real-ip
            - --grpc-cert-path=/app/certs
            - --grpc-ca-file=/app/ca.crt
            - --health-dial-server-name=ext-proc-real-ip.envoygateway
          ports:
            - containerPort: 9002
            - containerPort: 8080
          envFrom:
            - secretRef:
                name: tencent-eo-secret
          volumeMounts:
            - name: ext-proc-real-ip-certs
              mountPath: /app/certs
            - name: ext-proc-ca-cert
              mountPath: /app/ca.crt
              subPath: ca.crt
          readinessProbe:
            httpGet:
              path: /healthz
              port: 8080
      volumes:
        - name: ext-proc-real-ip-certs
          secret:
            secretName: ext-proc-real-ip-cert-secret
            items:
              - key: tls.crt
                path: server.crt
              - key: tls.key
                path: server.key
        - name: ext-proc-ca-cert
          secret:
            secretName: ext-proc-ca-cert-secret
            items:
              - key: ca.crt
                path: ca.crt
---
apiVersion: v1
kind: Service
metadata:
  name: ext-proc-real-ip
  namespace: envoy-gateway-system
spec:
  selector:
    app: ext-proc-real-ip
  ports:
    - protocol: TCP
      port: 9002
      targetPort: 9002
---
apiVersion: gateway.networking.k8s.io/v1
kind: BackendTLSPolicy
metadata:
  name: ext-proc-real-ip-btls
  namespace: envoy-gateway-system
spec:
  targetRefs:
    - kind: Service
      name: ext-proc-real-ip
      group: ""
  validation:
    caCertificateRefs:
      - name: ext-proc-ca-cert-secret
        kind: Secret
        group: ""
    hostname: ext-proc-real-ip.envoygateway
---
apiVersion: gateway.envoyproxy.io/v1alpha1
kind: EnvoyExtensionPolicy
metadata:
  name: envoy-extensions
  namespace: envoy-gateway-system
spec:
  targetRefs:
    - group: gateway.networking.k8s.io
      kind: Gateway
      name: envoy-gateway
  extProc:
    - backendRefs:
        - name: ext-proc-real-ip
          port: 9002
      processingMode:
        request:
          attributes:
            - request.id
            - source.address
        response:
          attributes:
            - request.id
            - source.address
      messageTimeout: 60s
```

## License

MIT, see [LICENSE](LICENSE).

    THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
    IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
    FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
    AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
    LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
    OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
    SOFTWARE.
