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
} from "lucide-react";
import {
  api,
  ApiError,
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
        </div>
        <div className="flex items-center gap-2">
          {!canEdit && <Badge variant="outline">read-only</Badge>}
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
    </div>
  );
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
