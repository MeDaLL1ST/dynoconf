package com.example.dynoconf;

import java.util.Map;
import java.util.TreeMap;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;

/**
 * Spring wiring for the dynoconf client. Exposes a single started
 * {@link DynoconfConfigClient} bean; inject it anywhere and read values through
 * {@code client.load().get("KEY")} on every use.
 */
@Configuration
public class DynoconfClientConfig {

    private static final Logger log = LoggerFactory.getLogger(DynoconfClientConfig.class);

    @Bean(destroyMethod = "stop")
    public DynoconfConfigClient dynoconfConfigClient(
            @Value("${config.service.addr:localhost:9090}") String addr,
            @Value("${config.service.key:}") String serviceKey,
            @Value("${config.watch.key:GREETING}") String watchKey) {

        if (serviceKey.isBlank()) {
            throw new IllegalStateException("config.service.key (CONFIG_SERVICE_KEY) is required");
        }

        // Defaults come from the app's env (its Kubernetes manifest) — the
        // graceful-degradation fallback if config-service is unavailable.
        Map<String, String> defaults = defaultsFromEnv();
        log.info("starting dynoconf client: addr={}, service={}, {} env defaults", addr, serviceKey, defaults.size());

        DynoconfConfigClient client = DynoconfConfigClient.builder()
                .addr(addr)
                .serviceKey(serviceKey)
                .defaults(defaults)
                .onSnapshot(all -> {
                    // The server sends ALL of the service's variables at once on
                    // (re)connect — the client holds every one, not just watchKey.
                    log.info("snapshot: {} variables now in config", all.size());
                    new TreeMap<>(all).forEach((k, v) -> log.info("    {} = {}", k, v));
                })
                .onChange((k, v, deleted) -> {
                    if (!k.equals(watchKey)) {
                        return; // "watch a specific key"
                    }
                    if (deleted) {
                        log.info("WATCH {} deleted -> fell back to default {}", k, v);
                    } else {
                        log.info("WATCH {} changed -> {}", k, v);
                    }
                })
                .build();

        client.start();
        return client;
    }

    /** Treats every UPPER_SNAKE_CASE env var as a config default (scope this in real apps). */
    private static Map<String, String> defaultsFromEnv() {
        Map<String, String> out = new TreeMap<>();
        System.getenv().forEach((k, v) -> {
            if (isConfigKey(k)) {
                out.put(k, v);
            }
        });
        return out;
    }

    private static boolean isConfigKey(String k) {
        if (k == null || k.isEmpty()
                || k.equals("CONFIG_SERVICE_ADDR") || k.equals("CONFIG_SERVICE_KEY") || k.equals("WATCH_KEY")) {
            return false;
        }
        return k.chars().allMatch(c -> (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_');
    }
}
