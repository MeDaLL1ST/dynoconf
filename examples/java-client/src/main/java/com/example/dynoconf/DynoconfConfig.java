package com.example.dynoconf;

import java.util.Collections;
import java.util.Map;

/**
 * Immutable snapshot of resolved configuration. Treat it as read-only; the
 * client replaces it wholesale on every change (see {@link DynoconfConfigClient#load()}).
 */
public final class DynoconfConfig {

    private final Map<String, String> values;

    DynoconfConfig(Map<String, String> values) {
        this.values = Collections.unmodifiableMap(values);
    }

    /** Returns the value for {@code key} (service override if present, otherwise the env default), or {@code null}. */
    public String get(String key) {
        return values.get(key);
    }

    /** Returns the value for {@code key}, or {@code def} if unset. */
    public String getOrDefault(String key, String def) {
        return values.getOrDefault(key, def);
    }

    /** Returns an unmodifiable view of the full key/value map. */
    public Map<String, String> all() {
        return values;
    }
}
