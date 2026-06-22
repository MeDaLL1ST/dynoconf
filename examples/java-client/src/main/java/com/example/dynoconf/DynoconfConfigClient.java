package com.example.dynoconf;

import java.util.HashMap;
import java.util.Map;
import java.util.Objects;
import java.util.concurrent.atomic.AtomicReference;
import java.util.logging.Logger;
import java.util.Iterator;

import dynoconf.v1.Change;
import dynoconf.v1.ConfigEvent;
import dynoconf.v1.ConfigStreamGrpc;
import dynoconf.v1.SubscribeRequest;
import dynoconf.v1.Variable;
import io.grpc.ManagedChannel;
import io.grpc.netty.shaded.io.grpc.netty.NettyChannelBuilder;

/**
 * Reference client that application teams copy into their Spring services. It
 * mirrors the Go reference client and demonstrates the target pattern:
 *
 * <ul>
 *   <li>read defaults from env;</li>
 *   <li>connect to config-service over gRPC with {@code send_snapshot=true};</li>
 *   <li>apply the snapshot on top of the defaults;</li>
 *   <li>subscribe to live changes and swap the in-memory config atomically;</li>
 *   <li>reconnect with backoff if the stream drops.</li>
 * </ul>
 *
 * Values are ALWAYS read through {@link #load()}, never captured into fields at
 * startup, so a change made in the UI takes effect at runtime without a restart.
 *
 * <p>This class is framework-agnostic; see {@code DynoconfClientConfig} for the
 * Spring {@code @Bean} wiring.
 */
public final class DynoconfConfigClient {

    /** Called once per (re)connect with the FULL resolved config after the snapshot is applied. */
    @FunctionalInterface
    public interface SnapshotListener {
        void onSnapshot(Map<String, String> all);
    }

    /**
     * Called for every incremental change after it is applied. {@code deleted=true}
     * means the variable was removed and the value fell back to the env default
     * (passed as {@code value}). This is the "watch" hook — filter by key inside.
     */
    @FunctionalInterface
    public interface ChangeListener {
        void onChange(String key, String value, boolean deleted);
    }

    private static final Logger log = Logger.getLogger(DynoconfConfigClient.class.getName());

    private final String addr;
    private final String serviceKey;
    private final Map<String, String> defaults;
    private final SnapshotListener onSnapshot;
    private final ChangeListener onChange;

    private final AtomicReference<DynoconfConfig> current = new AtomicReference<>();

    // overrides holds the latest server-provided values. Mutated only by the
    // single streaming thread, so no synchronization is needed for writes; reads
    // happen via the atomic config reference.
    private final Map<String, String> overrides = new HashMap<>();

    private volatile boolean running = false;
    private volatile ManagedChannel channel;
    private Thread worker;

    private DynoconfConfigClient(Builder b) {
        this.addr = Objects.requireNonNull(b.addr, "addr");
        this.serviceKey = Objects.requireNonNull(b.serviceKey, "serviceKey");
        this.defaults = b.defaults != null ? b.defaults : Map.of();
        this.onSnapshot = b.onSnapshot;
        this.onChange = b.onChange;
        rebuild(); // seed with defaults so load() is usable before the first connect
    }

    public static Builder builder() {
        return new Builder();
    }

    /** Returns the current configuration snapshot. Always read values through this. */
    public DynoconfConfig load() {
        return current.get();
    }

    /** Starts the background sync thread (idempotent). */
    public synchronized void start() {
        if (running) {
            return;
        }
        running = true;
        worker = new Thread(this::runLoop, "dynoconf-config-client");
        worker.setDaemon(true);
        worker.start();
    }

    /** Stops syncing and shuts the channel down. */
    public synchronized void stop() {
        running = false;
        ManagedChannel ch = channel;
        if (ch != null) {
            ch.shutdownNow();
        }
        if (worker != null) {
            worker.interrupt();
        }
    }

    private void runLoop() {
        long backoffMs = 1000;
        final long maxBackoffMs = 30_000;
        while (running) {
            try {
                stream();
                backoffMs = 1000;
            } catch (Exception e) {
                if (!running) {
                    return;
                }
                log.warning("config stream error: " + e.getMessage() + " (reconnecting in " + backoffMs + "ms)");
                try {
                    Thread.sleep(backoffMs);
                } catch (InterruptedException ie) {
                    return;
                }
                backoffMs = Math.min(backoffMs * 2, maxBackoffMs);
            }
        }
    }

    private void stream() {
        // v1: the gRPC endpoint is plaintext and cluster-internal. Swap to TLS
        // (NettyChannelBuilder.sslContext(...)) once the endpoint is secured.
        ManagedChannel ch = NettyChannelBuilder.forTarget(addr)
                .usePlaintext()
                .build();
        this.channel = ch;
        try {
            SubscribeRequest req = SubscribeRequest.newBuilder()
                    .setServiceKey(serviceKey)
                    .setSendSnapshot(true)
                    .build();

            Iterator<ConfigEvent> events = ConfigStreamGrpc.newBlockingStub(ch).subscribe(req);
            while (events.hasNext()) {
                handle(events.next());
            }
        } finally {
            ch.shutdownNow();
            this.channel = null;
        }
    }

    private void handle(ConfigEvent ev) {
        switch (ev.getEventCase()) {
            case SNAPSHOT -> {
                // First message after connect: the FULL set of the service's
                // variables. Replace all overrides wholesale, then notify.
                overrides.clear();
                for (Variable v : ev.getSnapshot().getVariablesList()) {
                    overrides.put(v.getKey(), v.getValue());
                }
                rebuild();
                log.info("applied snapshot: " + ev.getSnapshot().getVariablesCount() + " variables");
                if (onSnapshot != null) {
                    onSnapshot.onSnapshot(load().all());
                }
            }
            case CHANGE -> {
                Change change = ev.getChange();
                Variable v = change.getVariable();
                boolean deleted = change.getType() == Change.Type.DELETE;
                if (deleted) {
                    overrides.remove(v.getKey()); // fall back to env default
                } else {
                    overrides.put(v.getKey(), v.getValue());
                }
                rebuild();
                log.info("applied change: " + change.getType() + " " + v.getKey() + "=" + v.getValue() + " (v" + v.getVersion() + ")");
                if (onChange != null) {
                    // For deletes, report the value the key fell back to.
                    onChange.onChange(v.getKey(), load().get(v.getKey()), deleted);
                }
            }
            case HEARTBEAT -> { /* liveness only */ }
            case EVENT_NOT_SET -> { /* ignore */ }
        }
    }

    /** Recomputes the merged config (defaults overlaid with overrides) and swaps it in atomically. */
    private void rebuild() {
        Map<String, String> merged = new HashMap<>(defaults);
        merged.putAll(overrides);
        current.set(new DynoconfConfig(merged));
    }

    /** Builder for {@link DynoconfConfigClient}. */
    public static final class Builder {
        private String addr = "localhost:9090";
        private String serviceKey;
        private Map<String, String> defaults;
        private SnapshotListener onSnapshot;
        private ChangeListener onChange;

        public Builder addr(String addr) { this.addr = addr; return this; }
        public Builder serviceKey(String key) { this.serviceKey = key; return this; }
        public Builder defaults(Map<String, String> d) { this.defaults = d; return this; }
        public Builder onSnapshot(SnapshotListener l) { this.onSnapshot = l; return this; }
        public Builder onChange(ChangeListener l) { this.onChange = l; return this; }

        public DynoconfConfigClient build() {
            return new DynoconfConfigClient(this);
        }
    }
}
