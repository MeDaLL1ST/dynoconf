import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Copy, Check } from "lucide-react";
import { api, ApiError, type ConnectionInfo } from "@/lib/api";
import { Button, ErrorState, LoadingState } from "@/components/ui";
import { Modal } from "@/components/Modal";

export function ConnectionInfoModal({
  serviceId,
  onClose,
}: {
  serviceId: number;
  onClose: () => void;
}) {
  const info = useQuery<ConnectionInfo, ApiError>({
    queryKey: ["conn-info", serviceId],
    queryFn: () => api.connectionInfo(serviceId),
  });

  return (
    <Modal open onClose={onClose} title="Connection info">
      {info.isLoading && <LoadingState />}
      {info.error && <ErrorState message={info.error.message} />}
      {info.data && (
        <div className="space-y-4">
          <p className="text-sm text-muted-foreground">
            Apps connect over gRPC with this service key, send a snapshot request,
            then receive live changes. See <code>examples/go-client</code> for the
            full reference client.
          </p>
          <Snippet label="Service key" value={info.data.service_key} />
          <Snippet label="Env (Kubernetes)" value={info.data.env_snippet} multiline />
          <CodeTabs
            tabs={[
              { label: "Go", value: info.data.go_snippet },
              { label: "Java / Spring", value: info.data.java_snippet },
            ]}
          />
        </div>
      )}
    </Modal>
  );
}

function CodeTabs({ tabs }: { tabs: { label: string; value: string }[] }) {
  const [active, setActive] = useState(0);
  const tab = tabs[active];
  return (
    <div className="space-y-1.5">
      <div className="flex items-center justify-between">
        <div className="flex gap-1">
          {tabs.map((t, i) => (
            <button
              key={t.label}
              type="button"
              onClick={() => setActive(i)}
              className={`rounded-md px-2.5 py-1 text-xs font-medium transition-colors ${
                i === active
                  ? "bg-accent text-foreground"
                  : "text-muted-foreground hover:bg-accent/60"
              }`}
            >
              {t.label}
            </button>
          ))}
        </div>
        <CopyButton value={tab.value} />
      </div>
      <pre className="overflow-x-auto rounded-md border border-border bg-muted/40 p-3 font-mono text-xs whitespace-pre">
        {tab.value}
      </pre>
    </div>
  );
}

function CopyButton({ value }: { value: string }) {
  const [copied, setCopied] = useState(false);
  const copy = () => {
    navigator.clipboard.writeText(value);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };
  return (
    <Button size="sm" variant="ghost" onClick={copy}>
      {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
      {copied ? "Copied" : "Copy"}
    </Button>
  );
}

function Snippet({
  label,
  value,
  multiline,
}: {
  label: string;
  value: string;
  multiline?: boolean;
}) {
  const [copied, setCopied] = useState(false);
  const copy = () => {
    navigator.clipboard.writeText(value);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };
  return (
    <div className="space-y-1.5">
      <div className="flex items-center justify-between">
        <span className="text-sm font-medium">{label}</span>
        <Button size="sm" variant="ghost" onClick={copy}>
          {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
          {copied ? "Copied" : "Copy"}
        </Button>
      </div>
      <pre
        className={`overflow-x-auto rounded-md border border-border bg-muted/40 p-3 font-mono text-xs ${
          multiline ? "whitespace-pre" : "whitespace-nowrap"
        }`}
      >
        {value}
      </pre>
    </div>
  );
}
