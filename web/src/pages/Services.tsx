import { useMemo, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { Plus, Radio, ArrowRight, Star, Search as SearchIcon } from "lucide-react";
import { api, ApiError, type Me, type SearchHit, type Service } from "@/lib/api";
import { useEventStream } from "@/lib/sse";
import { cn } from "@/lib/utils";
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
  const [q, setQ] = useState("");
  const [tag, setTag] = useState<string | null>(null);

  useEventStream({
    onConnections: () => qc.invalidateQueries({ queryKey: ["services"] }),
  });

  const isAdmin = me.role === "admin";

  const allTags = useMemo(() => {
    const set = new Set<string>();
    services.data?.forEach((s) => s.tags?.forEach((t) => set.add(t)));
    return [...set].sort();
  }, [services.data]);

  const visible = useMemo(() => {
    let list = services.data ?? [];
    if (tag) list = list.filter((s) => s.tags?.includes(tag));
    // favorites first, then name
    return [...list].sort(
      (a, b) =>
        Number(b.is_favorite) - Number(a.is_favorite) || a.name.localeCompare(b.name)
    );
  }, [services.data, tag]);

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

      {/* Global search across services + variables. */}
      <div className="relative mb-4">
        <SearchIcon className="pointer-events-none absolute left-3 top-2.5 h-4 w-4 text-muted-foreground" />
        <Input
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder="Search variables by key or value across your services…"
          className="pl-9"
        />
      </div>
      {q.trim().length >= 2 && <SearchResults q={q.trim()} />}

      {allTags.length > 0 && (
        <div className="mb-4 flex flex-wrap items-center gap-1.5">
          <TagChip label="all" active={tag === null} onClick={() => setTag(null)} />
          {allTags.map((t) => (
            <TagChip key={t} label={t} active={tag === t} onClick={() => setTag(t)} />
          ))}
        </div>
      )}

      {services.isLoading && <LoadingState />}
      {services.error && <ErrorState message={services.error.message} />}
      {services.data && services.data.length === 0 && (
        <EmptyState
          title="No services yet"
          hint="Create a service to start managing its configuration. Admins can create services."
          action={
            isAdmin ? (
              <Button onClick={() => setCreating(true)}>
                <Plus className="h-4 w-4" /> New service
              </Button>
            ) : undefined
          }
        />
      )}

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {visible.map((s) => (
          <ServiceCard key={s.id} s={s} />
        ))}
      </div>

      <CreateServiceModal open={creating} onClose={() => setCreating(false)} />
    </div>
  );
}

function TagChip({
  label,
  active,
  onClick,
}: {
  label: string;
  active: boolean;
  onClick: () => void;
}) {
  return (
    <button
      onClick={onClick}
      className={cn(
        "rounded-full px-3 py-1 text-xs font-medium transition-colors",
        active ? "bg-primary text-primary-foreground" : "bg-secondary text-secondary-foreground hover:opacity-80"
      )}
    >
      {label}
    </button>
  );
}

function ServiceCard({ s }: { s: Service }) {
  const qc = useQueryClient();
  const fav = useMutation({
    mutationFn: () => (s.is_favorite ? api.removeFavorite(s.id) : api.addFavorite(s.id)),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["services"] }),
  });
  return (
    <Link to={`/services/${s.id}`}>
      <Card className="group h-full transition-colors hover:border-primary/50">
        <CardContent className="flex h-full flex-col gap-3 p-5">
          <div className="flex items-start justify-between gap-2">
            <div className="flex items-center gap-1.5">
              <button
                title={s.is_favorite ? "Unstar" : "Star"}
                onClick={(e) => {
                  e.preventDefault();
                  e.stopPropagation();
                  fav.mutate();
                }}
                className="text-muted-foreground hover:text-amber-500"
              >
                <Star className={cn("h-4 w-4", s.is_favorite && "fill-amber-500 text-amber-500")} />
              </button>
              <span className="font-semibold">{s.name}</span>
            </div>
            <ConnBadge count={s.active_connections} />
          </div>
          <code className="text-xs text-muted-foreground">{s.key}</code>
          {s.description && (
            <p className="line-clamp-2 text-sm text-muted-foreground">{s.description}</p>
          )}
          {s.tags?.length > 0 && (
            <div className="flex flex-wrap gap-1">
              {s.tags.map((t) => (
                <Badge key={t} variant="secondary">
                  {t}
                </Badge>
              ))}
            </div>
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
  );
}

function SearchResults({ q }: { q: string }) {
  const navigate = useNavigate();
  const res = useQuery<SearchHit[], ApiError>({
    queryKey: ["search", q],
    queryFn: () => api.search(q),
  });
  if (res.isLoading) return <div className="mb-4 text-sm text-muted-foreground">Searching…</div>;
  if (!res.data || res.data.length === 0)
    return <div className="mb-4 text-sm text-muted-foreground">No matches.</div>;
  return (
    <Card className="mb-4">
      <CardContent className="p-0">
        <table className="w-full text-sm">
          <tbody>
            {res.data.map((h, i) => (
              <tr
                key={i}
                className="cursor-pointer border-b border-border/60 last:border-0 hover:bg-accent/30"
                onClick={() => navigate(`/services/${h.service_id}`)}
              >
                <td className="px-4 py-2 text-muted-foreground">{h.service_name}</td>
                <td className="px-4 py-2 font-mono text-xs">{h.key}</td>
                <td className="px-4 py-2 font-mono text-xs text-muted-foreground">{h.value}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </CardContent>
    </Card>
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
