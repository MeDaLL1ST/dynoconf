import { useState } from "react";
import { useParams, Link, useNavigate } from "react-router-dom";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  Plus,
  Trash2,
  History,
  Check,
  X,
  Plug,
  ArrowLeft,
  Pencil,
  Users,
  ClipboardPaste,
} from "lucide-react";
import {
  api,
  ApiError,
  type ConnectionClient,
  type Me,
  type Service,
  type Variable,
} from "@/lib/api";
import { useEventStream } from "@/lib/sse";
import { formatTime, relativeTime } from "@/lib/utils";
import {
  Badge,
  Button,
  Card,
  CardContent,
  EmptyState,
  ErrorState,
  Input,
  LoadingState,
} from "@/components/ui";
import { Modal } from "@/components/Modal";
import { ConnBadge } from "./Services";
import { HistoryModal } from "./HistoryModal";
import { ConnectionInfoModal } from "./ConnectionInfoModal";

export function ServiceDetail({ me }: { me: Me }) {
  const { id } = useParams();
  const serviceId = Number(id);
  const qc = useQueryClient();
  const navigate = useNavigate();

  const service = useQuery<Service, ApiError>({
    queryKey: ["service", serviceId],
    queryFn: () => api.getService(serviceId),
  });
  const variables = useQuery<Variable[], ApiError>({
    queryKey: ["variables", serviceId],
    queryFn: () => api.listVariables(serviceId),
  });

  const [historyKey, setHistoryKey] = useState<string | null>(null);
  const [showConn, setShowConn] = useState(false);
  const [showClients, setShowClients] = useState(false);
  const [showBulk, setShowBulk] = useState(false);

  useEventStream({
    onVariable: (e) => {
      if (e.service_id === serviceId)
        qc.invalidateQueries({ queryKey: ["variables", serviceId] });
    },
    onConnections: (e) => {
      if (e.service_id === serviceId)
        qc.invalidateQueries({ queryKey: ["service", serviceId] });
    },
  });

  const canEdit = service.data?.access_level === "editor" || me.role === "admin";

  const del = useMutation({
    mutationFn: () => api.deleteService(serviceId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["services"] });
      navigate("/");
    },
  });

  if (service.isLoading) return <LoadingState />;
  if (service.error) return <ErrorState message={service.error.message} />;
  const svc = service.data!;

  return (
    <div>
      <Link
        to="/"
        className="mb-4 inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
      >
        <ArrowLeft className="h-4 w-4" /> Services
      </Link>

      <div className="mb-6 flex flex-wrap items-start justify-between gap-4">
        <div className="space-y-1">
          <div className="flex items-center gap-3">
            <h1 className="text-2xl font-semibold">{svc.name}</h1>
            <ConnBadge count={svc.active_connections} />
          </div>
          <code className="text-sm text-muted-foreground">{svc.key}</code>
          {svc.description && (
            <p className="max-w-2xl text-sm text-muted-foreground">{svc.description}</p>
          )}
          <TagsEditor svc={svc} canEdit={canEdit} />
        </div>
        <div className="flex items-center gap-2">
          {!canEdit && <Badge variant="outline">read-only</Badge>}
          <Button variant="outline" onClick={() => setShowClients(true)}>
            <Users className="h-4 w-4" /> Clients ({svc.active_connections})
          </Button>
          {canEdit && (
            <Button variant="outline" onClick={() => setShowBulk(true)}>
              <ClipboardPaste className="h-4 w-4" /> Bulk edit
            </Button>
          )}
          <Button variant="outline" onClick={() => setShowConn(true)}>
            <Plug className="h-4 w-4" /> Connection info
          </Button>
          {me.role === "admin" && (
            <Button
              variant="destructive"
              onClick={() => {
                if (confirm(`Delete service "${svc.name}"? This cannot be undone.`))
                  del.mutate();
              }}
            >
              <Trash2 className="h-4 w-4" /> Delete
            </Button>
          )}
        </div>
      </div>

      <Card>
        <CardContent className="p-0">
          {variables.isLoading && <LoadingState />}
          {variables.error && (
            <div className="p-5">
              <ErrorState message={variables.error.message} />
            </div>
          )}
          {variables.data && (
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border text-left text-xs uppercase tracking-wide text-muted-foreground">
                  <th className="px-5 py-3 font-medium">Key</th>
                  <th className="px-5 py-3 font-medium">Value</th>
                  <th className="px-5 py-3 font-medium">Ver</th>
                  <th className="px-5 py-3 font-medium">Changed by</th>
                  <th className="px-5 py-3 font-medium">When</th>
                  <th className="px-5 py-3" />
                </tr>
              </thead>
              <tbody>
                {variables.data.map((v) => (
                  <VariableRow
                    key={v.key}
                    serviceId={serviceId}
                    variable={v}
                    canEdit={canEdit}
                    onHistory={() => setHistoryKey(v.key)}
                  />
                ))}
                {canEdit && <AddVariableRow serviceId={serviceId} />}
              </tbody>
            </table>
          )}
          {variables.data?.length === 0 && !canEdit && (
            <div className="p-5">
              <EmptyState
                title="No variables"
                hint="This service has no configuration variables yet."
              />
            </div>
          )}
        </CardContent>
      </Card>

      {historyKey && (
        <HistoryModal
          serviceId={serviceId}
          variableKey={historyKey}
          canEdit={canEdit}
          onClose={() => setHistoryKey(null)}
        />
      )}
      {showConn && (
        <ConnectionInfoModal serviceId={serviceId} onClose={() => setShowConn(false)} />
      )}
      {showClients && (
        <ClientsModal serviceId={serviceId} onClose={() => setShowClients(false)} />
      )}
      {showBulk && (
        <BulkEditModal serviceId={serviceId} onClose={() => setShowBulk(false)} />
      )}
    </div>
  );
}

function TagsEditor({ svc, canEdit }: { svc: Service; canEdit: boolean }) {
  const qc = useQueryClient();
  const [editing, setEditing] = useState(false);
  const [text, setText] = useState((svc.tags ?? []).join(", "));
  const save = useMutation({
    mutationFn: () =>
      api.setTags(
        svc.id,
        text.split(",").map((t) => t.trim()).filter(Boolean)
      ),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["service", svc.id] });
      qc.invalidateQueries({ queryKey: ["services"] });
      setEditing(false);
    },
  });
  if (editing) {
    return (
      <div className="flex items-center gap-1.5 pt-1">
        <Input
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder="tag1, tag2"
          className="h-8 w-64"
          autoFocus
        />
        <Button size="icon" className="h-8 w-8" onClick={() => save.mutate()}>
          <Check className="h-4 w-4" />
        </Button>
        <Button size="icon" variant="outline" className="h-8 w-8" onClick={() => setEditing(false)}>
          <X className="h-4 w-4" />
        </Button>
      </div>
    );
  }
  return (
    <div className="flex flex-wrap items-center gap-1 pt-1">
      {(svc.tags ?? []).map((t) => (
        <Badge key={t} variant="secondary">
          {t}
        </Badge>
      ))}
      {canEdit && (
        <button
          onClick={() => {
            setText((svc.tags ?? []).join(", "));
            setEditing(true);
          }}
          className="text-xs text-muted-foreground hover:text-foreground"
        >
          <Pencil className="inline h-3 w-3" /> tags
        </button>
      )}
    </div>
  );
}

function ClientsModal({ serviceId, onClose }: { serviceId: number; onClose: () => void }) {
  const clients = useQuery<ConnectionClient[], ApiError>({
    queryKey: ["clients", serviceId],
    queryFn: () => api.serviceClients(serviceId),
    refetchInterval: 5000,
  });
  const byReplica: Record<string, ConnectionClient[]> = {};
  (clients.data ?? []).forEach((c) => {
    (byReplica[c.replica_id] ||= []).push(c);
  });
  return (
    <Modal open onClose={onClose} title="Active gRPC clients">
      {clients.isLoading && <LoadingState />}
      {clients.error && <ErrorState message={clients.error.message} />}
      {clients.data && clients.data.length === 0 && (
        <p className="text-sm text-muted-foreground">No active connections.</p>
      )}
      <div className="space-y-4">
        {Object.entries(byReplica).map(([replica, list]) => (
          <div key={replica}>
            <div className="mb-1 text-xs font-medium text-muted-foreground">
              replica <code>{replica}</code> · {list.length} connection(s)
            </div>
            <ul className="space-y-1">
              {list.map((c) => (
                <li key={c.conn_id} className="flex justify-between rounded-md border border-border px-3 py-1.5 text-xs">
                  <span className="font-mono">{c.peer_addr}</span>
                  <span className="text-muted-foreground" title={formatTime(c.connected_at)}>
                    since {relativeTime(c.connected_at)}
                  </span>
                </li>
              ))}
            </ul>
          </div>
        ))}
      </div>
    </Modal>
  );
}

function BulkEditModal({ serviceId, onClose }: { serviceId: number; onClose: () => void }) {
  const qc = useQueryClient();
  const [text, setText] = useState("");
  const apply = useMutation({
    mutationFn: () => api.bulkUpsert(serviceId, parseEnv(text)),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["variables", serviceId] });
      onClose();
    },
  });
  const parsed = parseEnv(text);
  return (
    <Modal
      open
      onClose={onClose}
      title="Bulk edit (paste KEY=VALUE lines)"
      footer={
        <>
          <Button variant="outline" onClick={onClose}>
            Cancel
          </Button>
          <Button
            onClick={() => apply.mutate()}
            disabled={Object.keys(parsed).length === 0 || apply.isPending}
          >
            Apply {Object.keys(parsed).length} variable(s)
          </Button>
        </>
      }
    >
      <div className="space-y-2">
        <textarea
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder={"DB_HOST=db1\nLOG_LEVEL=info\n# lines starting with # are ignored"}
          className="h-48 w-full rounded-md border border-input bg-background p-3 font-mono text-xs focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        />
        <p className="text-xs text-muted-foreground">
          Applied in a single transaction; each is versioned. Existing variables not
          listed are left untouched.
        </p>
        {apply.error && <ErrorState message={(apply.error as ApiError).message} />}
      </div>
    </Modal>
  );
}

function parseEnv(text: string): Record<string, string> {
  const out: Record<string, string> = {};
  for (const raw of text.split("\n")) {
    const line = raw.trim();
    if (!line || line.startsWith("#")) continue;
    const eq = line.indexOf("=");
    if (eq <= 0) continue;
    let val = line.slice(eq + 1).trim();
    if (val.length >= 2 && val.startsWith('"') && val.endsWith('"')) {
      val = val.slice(1, -1);
    }
    out[line.slice(0, eq).trim()] = val;
  }
  return out;
}

function VariableRow({
  serviceId,
  variable,
  canEdit,
  onHistory,
}: {
  serviceId: number;
  variable: Variable;
  canEdit: boolean;
  onHistory: () => void;
}) {
  const qc = useQueryClient();
  const [editing, setEditing] = useState(false);
  const [value, setValue] = useState(variable.value);

  const save = useMutation({
    mutationFn: () => api.putVariable(serviceId, variable.key, value),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["variables", serviceId] });
      setEditing(false);
    },
  });
  const del = useMutation({
    mutationFn: () => api.deleteVariable(serviceId, variable.key),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["variables", serviceId] }),
  });

  return (
    <tr className="border-b border-border/60 last:border-0 hover:bg-accent/30">
      <td className="px-5 py-2.5 align-top font-mono text-xs">{variable.key}</td>
      <td className="px-5 py-2.5 align-top">
        {editing ? (
          <div className="flex items-center gap-1.5">
            <Input
              value={value}
              onChange={(e) => setValue(e.target.value)}
              className="h-8 font-mono"
              autoFocus
              onKeyDown={(e) => {
                if (e.key === "Enter") save.mutate();
                if (e.key === "Escape") {
                  setValue(variable.value);
                  setEditing(false);
                }
              }}
            />
            <Button size="icon" className="h-8 w-8" onClick={() => save.mutate()}>
              <Check className="h-4 w-4" />
            </Button>
            <Button
              size="icon"
              variant="outline"
              className="h-8 w-8"
              onClick={() => {
                setValue(variable.value);
                setEditing(false);
              }}
            >
              <X className="h-4 w-4" />
            </Button>
          </div>
        ) : (
          <button
            type="button"
            disabled={!canEdit}
            onClick={() => canEdit && setEditing(true)}
            className="group flex max-w-md items-center gap-2 text-left font-mono text-xs"
          >
            <span className="break-all">{variable.value || <em className="text-muted-foreground">empty</em>}</span>
            {canEdit && (
              <Pencil className="h-3 w-3 shrink-0 text-muted-foreground opacity-0 group-hover:opacity-100" />
            )}
          </button>
        )}
      </td>
      <td className="px-5 py-2.5 align-top">
        <Badge variant="secondary">v{variable.version}</Badge>
      </td>
      <td className="px-5 py-2.5 align-top text-muted-foreground">{variable.updated_by}</td>
      <td className="px-5 py-2.5 align-top text-muted-foreground" title={formatTime(variable.updated_at)}>
        {relativeTime(variable.updated_at)}
      </td>
      <td className="px-5 py-2.5 align-top">
        <div className="flex justify-end gap-1">
          <Button size="icon" variant="ghost" className="h-8 w-8" onClick={onHistory} title="History">
            <History className="h-4 w-4" />
          </Button>
          {canEdit && (
            <Button
              size="icon"
              variant="ghost"
              className="h-8 w-8 text-destructive"
              title="Delete"
              onClick={() => {
                if (confirm(`Delete variable "${variable.key}"?`)) del.mutate();
              }}
            >
              <Trash2 className="h-4 w-4" />
            </Button>
          )}
        </div>
      </td>
    </tr>
  );
}

function AddVariableRow({ serviceId }: { serviceId: number }) {
  const qc = useQueryClient();
  const [key, setKey] = useState("");
  const [value, setValue] = useState("");

  const add = useMutation({
    mutationFn: () => api.putVariable(serviceId, key, value),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["variables", serviceId] });
      setKey("");
      setValue("");
    },
  });

  const submit = () => {
    if (key.trim()) add.mutate();
  };

  return (
    <tr className="bg-accent/20">
      <td className="px-5 py-2.5">
        <Input
          value={key}
          onChange={(e) => setKey(e.target.value)}
          placeholder="NEW_KEY"
          className="h-8 font-mono"
          onKeyDown={(e) => e.key === "Enter" && submit()}
        />
      </td>
      <td className="px-5 py-2.5" colSpan={4}>
        <Input
          value={value}
          onChange={(e) => setValue(e.target.value)}
          placeholder="value"
          className="h-8 font-mono"
          onKeyDown={(e) => e.key === "Enter" && submit()}
        />
      </td>
      <td className="px-5 py-2.5">
        <div className="flex justify-end">
          <Button size="sm" onClick={submit} disabled={!key.trim() || add.isPending}>
            <Plus className="h-4 w-4" /> Add
          </Button>
        </div>
      </td>
    </tr>
  );
}
