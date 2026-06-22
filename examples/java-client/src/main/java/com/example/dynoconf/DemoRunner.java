package com.example.dynoconf;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.scheduling.annotation.Scheduled;
import org.springframework.stereotype.Component;

/**
 * Periodically prints the watched key so you can change it in the UI and watch
 * it update here at runtime — no restart. Demonstrates reading through
 * {@link DynoconfConfigClient#load()} on every access.
 */
@Component
public class DemoRunner {

    private static final Logger log = LoggerFactory.getLogger(DemoRunner.class);

    private final DynoconfConfigClient client;
    private final String watchKey;

    public DemoRunner(DynoconfConfigClient client,
                      @Value("${config.watch.key:GREETING}") String watchKey) {
        this.client = client;
        this.watchKey = watchKey;
    }

    @Scheduled(fixedRate = 5000)
    public void poll() {
        DynoconfConfig cfg = client.load();
        log.info("[poll] {} = {} ({} keys total)", watchKey, cfg.get(watchKey), cfg.all().size());
    }
}
