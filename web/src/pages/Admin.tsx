import { useRef, useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { Trash2, UserPlus, Download, Upload, KeyRound, Copy } from "lucide-react";
import {
  api,
  ApiError,
  type ApiToken,
  type Me,
  type Permission,
  type Service,
  type User,
} from "@/lib/api";
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  ErrorState,
  Input,
  LoadingState,
} from "@/components/ui";
import { formatTime } from "@/lib/utils";

export function Admin({ me }: { me: Me }) {
  return (
    <div className="space-y-8">
      <div>
        <h1 className="text-2xl font-semibold">Admin</h1>
        <p className="text-sm text-muted-foreground">
          Manage users, global roles, and per-service access.
        </p>
      </div>
      <Users me={me} />
      <ServiceAccess />
      <ConfigTransfer />
      <ApiTokens />
    </div>
  );
}

function ApiTokens() {
  const qc = useQueryClient();
  const tokens = useQuery<ApiToken[], ApiError>({ queryKey: ["tokens"], queryFn: api.listTokens });
  const [name, setName] = useState("");
  const [created, setCreated] = useState<string | null>(null);

  const create = useMutation({
    mutationFn: () => api.createToken(name),
    onSuccess: (r) => {
      setCreated(r.token);
      setName("");
      qc.invalidateQueries({ queryKey: ["tokens"] });
    },
  });
  const del = useMutation({
    mutationFn: (id: number) => api.deleteToken(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["tokens"] }),
  });

  return (
    <Card>
      <CardHeader>
        <h2 className="text-lg font-semibold">API tokens (CLI / CI)</h2>
        <p className="text-sm text-muted-foreground">
          Personal tokens for <code>dynoconf-cli</code> and CI. A token inherits your
          access. Shown once at creation — store it safely.
        </p>
      </CardHeader>
      <CardContent className="space-y-4">
        {created && (
          <div className="space-y-1 rounded-md border border-emerald-500/40 bg-emerald-500/10 p-3">
            <div className="text-sm font-medium text-emerald-500">New token (copy now):</div>
            <div className="flex items-center gap-2">
              <code className="break-all text-xs">{created}</code>
              <Button size="sm" variant="ghost" onClick={() => navigator.clipboard.writeText(created)}>
                <Copy className="h-3.5 w-3.5" />
              </Button>
            </div>
          </div>
        )}
        <div className="flex items-end gap-2">
          <label className="flex-1 space-y-1.5">
            <span className="text-sm font-medium">Token name</span>
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="ci-pipeline" />
          </label>
          <Button onClick={() => create.mutate()} disabled={!name || create.isPending}>
            <KeyRound className="h-4 w-4" /> Create
          </Button>
        </div>
        {create.error && <ErrorState message={(create.error as ApiError).message} />}

        {tokens.data && tokens.data.length > 0 && (
          <table className="w-full text-sm">
            <tbody>
              {tokens.data.map((t) => (
                <tr key={t.id} className="border-b border-border/60 last:border-0">
                  <td className="py-2.5">{t.name}</td>
                  <td className="py-2.5 text-xs text-muted-foreground">
                    {t.last_used_at ? "last used " + formatTime(t.last_used_at) : "never used"}
                  </td>
                  <td className="py-2.5 text-right">
                    <Button
                      size="icon"
                      variant="ghost"
                      className="h-8 w-8 text-destructive"
                      onClick={() => del.mutate(t.id)}
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </CardContent>
    </Card>
  );
}

function ConfigTransfer() {
  const qc = useQueryClient();
  const fileRef = useRef<HTMLInputElement>(null);
  const [result, setResult] = useState<string | null>(null);

  const importMut = useMutation({
    mutationFn: (text: string) => api.importConfig(text),
    onSuccess: (r) => {
      setResult(
        `Imported: ${r.variables_imported} variables, ${r.services_created} new service(s), ${r.services_existing} existing.`
      );
      qc.invalidateQueries({ queryKey: ["services"] });
    },
  });

  const onFile = async (file: File) => {
    setResult(null);
    const text = await file.text();
    importMut.mutate(text);
  };

  return (
    <Card>
      <CardHeader>
        <h2 className="text-lg font-semibold">Export / import configuration</h2>
        <p className="text-sm text-muted-foreground">
          Download every service and its current variables as JSON, then import it
          on another contour. Import is a merge: it creates missing services and
          upserts variables (versioned and audited); it never deletes.
        </p>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="flex flex-wrap items-center gap-3">
          {/* Direct download — browser handles it via Content-Disposition. */}
          <a
            href="/api/export"
            className="inline-flex h-9 items-center justify-center gap-2 rounded-md bg-secondary px-4 text-sm font-medium text-secondary-foreground hover:opacity-80"
          >
            <Download className="h-4 w-4" /> Export JSON
          </a>
          <Button onClick={() => fileRef.current?.click()} disabled={importMut.isPending}>
            <Upload className="h-4 w-4" /> {importMut.isPending ? "Importing…" : "Import JSON"}
          </Button>
          <input
            ref={fileRef}
            type="file"
            accept="application/json,.json"
            className="hidden"
            onChange={(e) => {
              const f = e.target.files?.[0];
              if (f) onFile(f);
              e.target.value = "";
            }}
          />
        </div>
        {importMut.error && <ErrorState message={(importMut.error as ApiError).message} />}
        {result && (
          <div className="rounded-md border border-emerald-500/40 bg-emerald-500/10 px-4 py-3 text-sm text-emerald-500">
            {result}
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function Users({ me }: { me: Me }) {
  const qc = useQueryClient();
  const users = useQuery<User[], ApiError>({ queryKey: ["users"], queryFn: api.listUsers });
  const setRole = useMutation({
    mutationFn: (v: { id: number; role: "admin" | "user" }) =>
      api.setUserRole(v.id, v.role),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["users"] }),
  });

  return (
    <Card>
      <CardHeader>
        <h2 className="text-lg font-semibold">Users</h2>
        <p className="text-sm text-muted-foreground">
          Users are provisioned on first login. Admins have full access to every
          service.
        </p>
      </CardHeader>
      <CardContent>
        {users.isLoading && <LoadingState />}
        {users.error && <ErrorState message={users.error.message} />}
        {users.data && (
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border text-left text-xs uppercase tracking-wide text-muted-foreground">
                <th className="py-2 font-medium">Email</th>
                <th className="py-2 font-medium">Name</th>
                <th className="py-2 font-medium">Role</th>
                <th className="py-2" />
              </tr>
            </thead>
            <tbody>
              {users.data.map((u) => (
                <tr key={u.id} className="border-b border-border/60 last:border-0">
                  <td className="py-2.5">{u.email}</td>
                  <td className="py-2.5 text-muted-foreground">{u.name}</td>
                  <td className="py-2.5">
                    <Badge variant={u.role === "admin" ? "success" : "outline"}>
                      {u.role}
                    </Badge>
                  </td>
                  <td className="py-2.5 text-right">
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={u.id === me.id}
                      title={u.id === me.id ? "You can't change your own role" : ""}
                      onClick={() =>
                        setRole.mutate({
                          id: u.id,
                          role: u.role === "admin" ? "user" : "admin",
                        })
                      }
                    >
                      {u.role === "admin" ? "Make user" : "Make admin"}
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </CardContent>
    </Card>
  );
}

function ServiceAccess() {
  const qc = useQueryClient();
  const services = useQuery<Service[], ApiError>({
    queryKey: ["services"],
    queryFn: api.listServices,
  });
  const [serviceId, setServiceId] = useState<number | null>(null);
  const sid = serviceId ?? services.data?.[0]?.id ?? null;

  const perms = useQuery<Permission[], ApiError>({
    queryKey: ["permissions", sid],
    queryFn: () => api.listPermissions(sid!),
    enabled: sid != null,
  });

  const [email, setEmail] = useState("");
  const [level, setLevel] = useState<"viewer" | "editor">("viewer");

  const grant = useMutation({
    mutationFn: () => api.setPermission(sid!, email, level),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["permissions", sid] });
      setEmail("");
    },
  });
  const revoke = useMutation({
    mutationFn: (userId: number) => api.revokePermission(sid!, userId),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["permissions", sid] }),
  });

  return (
    <Card>
      <CardHeader>
        <h2 className="text-lg font-semibold">Service access</h2>
        <p className="text-sm text-muted-foreground">
          Grant viewer (read) or editor (read + write) access to a specific service.
        </p>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-sm font-medium">Service</span>
          <select
            className="h-9 rounded-md border border-input bg-background px-3 text-sm"
            value={sid ?? ""}
            onChange={(e) => setServiceId(Number(e.target.value))}
          >
            {services.data?.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name} ({s.key})
              </option>
            ))}
          </select>
        </div>

        {perms.isLoading && <LoadingState />}
        {perms.error && <ErrorState message={perms.error.message} />}
        {grant.error && <ErrorState message={(grant.error as ApiError).message} />}

        {sid != null && (
          <>
            <table className="w-full text-sm">
              <tbody>
                {perms.data?.length === 0 && (
                  <tr>
                    <td className="py-3 text-muted-foreground" colSpan={3}>
                      No one has explicit access yet (admins always have access).
                    </td>
                  </tr>
                )}
                {perms.data?.map((p) => (
                  <tr key={p.user_id} className="border-b border-border/60 last:border-0">
                    <td className="py-2.5">{p.email}</td>
                    <td className="py-2.5">
                      <Badge variant={p.level === "editor" ? "success" : "outline"}>
                        {p.level}
                      </Badge>
                    </td>
                    <td className="py-2.5 text-right">
                      <Button
                        size="icon"
                        variant="ghost"
                        className="h-8 w-8 text-destructive"
                        onClick={() => revoke.mutate(p.user_id)}
                      >
                        <Trash2 className="h-4 w-4" />
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>

            <div className="flex flex-wrap items-end gap-2 border-t border-border pt-4">
              <label className="flex-1 space-y-1.5">
                <span className="text-sm font-medium">User email</span>
                <Input
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  placeholder="dev@example.com"
                />
              </label>
              <label className="space-y-1.5">
                <span className="text-sm font-medium">Level</span>
                <select
                  className="block h-9 rounded-md border border-input bg-background px-3 text-sm"
                  value={level}
                  onChange={(e) => setLevel(e.target.value as "viewer" | "editor")}
                >
                  <option value="viewer">viewer</option>
                  <option value="editor">editor</option>
                </select>
              </label>
              <Button onClick={() => grant.mutate()} disabled={!email || grant.isPending}>
                <UserPlus className="h-4 w-4" /> Grant
              </Button>
            </div>
            <p className="text-xs text-muted-foreground">
              The user must have logged in at least once before you can grant access.
            </p>
          </>
        )}
      </CardContent>
    </Card>
  );
}
