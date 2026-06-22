import { useEffect } from "react";

export interface VariableEvent {
  kind: "var";
  service_id: number;
  change_type: "upsert" | "delete";
  key: string;
  value: string;
  version: number;
}

export interface ConnectionsEvent {
  kind: "conns";
  service_id: number;
}

type Handlers = {
  onVariable?: (e: VariableEvent) => void;
  onConnections?: (e: ConnectionsEvent) => void;
};

// useEventStream subscribes to the server SSE feed for the lifetime of the
// component. The Go server pushes `variable` and `connections` events.
export function useEventStream(handlers: Handlers) {
  useEffect(() => {
    const es = new EventSource("/api/events", { withCredentials: true });

    es.addEventListener("variable", (ev) => {
      try {
        handlers.onVariable?.(JSON.parse((ev as MessageEvent).data));
      } catch {
        /* ignore */
      }
    });
    es.addEventListener("connections", (ev) => {
      try {
        handlers.onConnections?.(JSON.parse((ev as MessageEvent).data));
      } catch {
        /* ignore */
      }
    });

    return () => es.close();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
}
