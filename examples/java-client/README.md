# dynoconf — Java / Spring reference client

Mirror of `examples/go-client` for Spring services. gRPC stubs are generated at
build time from the canonical [`proto/config.proto`](../../proto/config.proto)
(no checked-in stubs).

## Run

With the stack up (`docker compose up` in the repo root) and a service created in
the UI (e.g. key `payments-api` with a `GREETING` variable):

```bash
cd examples/java-client
CONFIG_SERVICE_ADDR=localhost:9090 \
CONFIG_SERVICE_KEY=payments-api \
WATCH_KEY=GREETING \
GREETING="env default" \
mvn spring-boot:run
```

On connect it logs the **full snapshot** (all the service's variables at once),
then logs each change. Change `GREETING` in the UI and watch it update here
within seconds — no restart. Delete it and it falls back to the env default.

## How to use it in your service

`DynoconfConfigClient` is framework-agnostic; `DynoconfClientConfig` shows the
Spring `@Bean` wiring. Inject the client and **read on every use** — never cache
a value at startup:

```java
@Service
class PricingService {
    private final DynoconfConfigClient config;
    PricingService(DynoconfConfigClient config) { this.config = config; }

    BigDecimal feeRate() {
        return new BigDecimal(config.load().getOrDefault("FEE_RATE", "0.02"));
    }
}
```

The client applies the snapshot over your env defaults, swaps the config
atomically on every change (`AtomicReference<DynoconfConfig>`), falls back to the
env default when a variable is deleted, and reconnects with backoff if the
stream drops.

> v1 uses plaintext gRPC (cluster-internal). Switch `NettyChannelBuilder` to TLS
> once the endpoint is secured.
