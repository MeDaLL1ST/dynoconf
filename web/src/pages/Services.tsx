import { useState } from "react";
import { Link } from "react-router-dom";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { Plus, Radio, ArrowRight } from "lucide-react";
import { api, ApiError, type Me, type Service } from "@/lib/api";
import { useEventStream } from "@/lib/sse";
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

export function Services({ me }: { me: Me }) {
  const qc = useQueryClient();
  const services = useQuery<Service[], ApiError>({
    queryKey: ["services"],
    queryFn: api.listServices,
  });
  const [creating, setCreating] = useState(false);

  // Live connection counts.
  useEventStream({
    onConnections: () => qc.invalidateQueries({ queryKey: ["services"] }),
  });

  const isAdmin = me.role === "admin";

  return (
    <div>
      <div className="mb-6 flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold">Services</h1>
          <p className="text-sm text-muted-foreground">
            Configuration is delivered to apps over gRPC and overrides their env
            defaults.
          </p>
        </div>
        {isAdmin && (
          <Button onClick={() => setCreating(true)}>
            <Plus className="h-4 w-4" /> New service
          </Button>
        )}
      </div>

      {services.isLoading && <LoadingState />}
      {services.error && <ErrorState message={services.error.message} />}
      {services.data && services.data.length === 0 && (
        <EmptyState
          title="No services yet"
          hint="Create a service to start managing its configuration. Admins can create services."
          action={
            <Button onClick={() => setCreating(true)}>
              <Plus className="h-4 w-4" /> New service
            </Button>
          }
        />
      )}

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {services.data?.map((s) => (
          <Link key={s.id} to={`/services/${s.id}`}>
            <Card className="group h-full transition-colors hover:border-primary/50">
              <CardContent className="flex h-full flex-col gap-3 p-5">
                <div className="flex items-start justify-between">
                  <div className="font-semibold">{s.name}</div>
                  <ConnBadge count={s.active_connections} />
                </div>
                <code className="text-xs text-muted-foreground">{s.key}</code>
                {s.description && (
                  <p className="line-clamp-2 text-sm text-muted-foreground">
                    {s.description}
                  </p>
                )}
                <div className="mt-auto flex items-center justify-between pt-2">
                  <Badge variant={s.access_level === "editor" ? "success" : "outline"}>
                    {s.access_level || "no access"}
                  </Badge>
                  <ArrowRight className="h-4 w-4 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100" />
                </div>
              </CardContent>
            </Card>
          </Link>
        ))}
      </div>

      <CreateServiceModal open={creating} onClose={() => setCreating(false)} />
    </div>
  );
}

export function ConnBadge({ count }: { count: number }) {
  return (
    <Badge variant={count > 0 ? "success" : "outline"} title="Active gRPC connections">
      <Radio className="mr-1 h-3 w-3" />
      {count}
    </Badge>
  );
}

function CreateServiceModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const qc = useQueryClient();
  const [name, setName] = useState("");
  const [key, setKey] = useState("");
  const [description, setDescription] = useState("");

  const create = useMutation({
    mutationFn: () => api.createService({ name, key: key || undefined, description }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["services"] });
      setName("");
      setKey("");
      setDescription("");
      onClose();
    },
  });

  return (
    <Modal
      open={open}
      onClose={onClose}
      title="New service"
      footer={
        <>
          <Button variant="outline" onClick={onClose}>
            Cancel
          </Button>
          <Button onClick={() => create.mutate()} disabled={!name || create.isPending}>
            Create
          </Button>
        </>
      }
    >
      <div className="space-y-3">
        <Field label="Name">
          <Input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="Payments API"
            autoFocus
          />
        </Field>
        <Field label="Service key (optional — generated if blank)">
          <Input
            value={key}
            onChange={(e) => setKey(e.target.value)}
            placeholder="payments-api"
            className="font-mono"
          />
        </Field>
        <Field label="Description (optional)">
          <Input
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder="Handles checkout and refunds"
          />
        </Field>
        {create.error && (
          <ErrorState message={(create.error as ApiError).message} />
        )}
      </div>
    </Modal>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="block space-y-1.5">
      <span className="text-sm font-medium">{label}</span>
      {children}
    </label>
  );
}
