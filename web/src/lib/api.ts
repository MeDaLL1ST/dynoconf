// Typed REST client for the dynoconf API. All requests are same-origin and
// authenticated by the session cookie.

export interface Me {
  id: number;
  email: string;
  name: string;
  role: "admin" | "user";
  contour: string;
}

export interface Service {
  id: number;
  key: string;
  name: string;
  description: string;
  created_at: string;
  created_by: string;
  active_connections: number;
  access_level: "viewer" | "editor" | "";
}

export interface Variable {
  id: number;
  service_id: number;
  key: string;
  value: string;
  version: number;
  updated_at: string;
  updated_by: string;
}

export interface VariableVersion {
  id: number;
  service_id: number;
  key: string;
  value: string;
  version: number;
  change_type: "create" | "update" | "delete" | "rollback";
  changed_at: string;
  changed_by: string;
}

export interface AuditEntry {
  id: number;
  actor: string;
  action: string;
  target: string;
  details: Record<string, unknown>;
  created_at: string;
}

export interface User {
  id: number;
  email: string;
  name: string;
  role: "admin" | "user";
  created_at: string;
}

export interface Permission {
  user_id: number;
  email: string;
  name: string;
  level: "viewer" | "editor";
}

export interface ConnectionInfo {
  service_key: string;
  grpc_addr: string;
  contour: string;
  env_snippet: string;
  go_snippet: string;
  java_snippet: string;
}

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

async function req<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: body ? { "Content-Type": "application/json" } : undefined,
    body: body ? JSON.stringify(body) : undefined,
    credentials: "same-origin",
  });
  if (!res.ok) {
    let msg = res.statusText;
    try {
      const j = await res.json();
      if (j.error) msg = j.error;
    } catch {
      /* ignore */
    }
    throw new ApiError(res.status, msg);
  }
  if (res.status === 204) return undefined as T;
  const text = await res.text();
  return text ? (JSON.parse(text) as T) : (undefined as T);
}

export const api = {
  me: () => req<Me>("GET", "/api/me"),

  listServices: () => req<Service[]>("GET", "/api/services"),
  getService: (id: number) => req<Service>("GET", `/api/services/${id}`),
  createService: (b: { key?: string; name: string; description?: string }) =>
    req<Service>("POST", "/api/services", b),
  deleteService: (id: number) => req<void>("DELETE", `/api/services/${id}`),
  connectionInfo: (id: number) =>
    req<ConnectionInfo>("GET", `/api/services/${id}/connection-info`),
  serviceConnections: (id: number) =>
    req<{ active_connections: number }>("GET", `/api/services/${id}/connections`),

  listVariables: (id: number) =>
    req<Variable[]>("GET", `/api/services/${id}/variables`),
  putVariable: (id: number, key: string, value: string) =>
    req<Variable>("PUT", `/api/services/${id}/variables/${encodeURIComponent(key)}`, {
      value,
    }),
  deleteVariable: (id: number, key: string) =>
    req<void>("DELETE", `/api/services/${id}/variables/${encodeURIComponent(key)}`),

  serviceHistory: (id: number) =>
    req<VariableVersion[]>("GET", `/api/services/${id}/history`),
  variableHistory: (id: number, key: string) =>
    req<VariableVersion[]>(
      "GET",
      `/api/services/${id}/variables/${encodeURIComponent(key)}/history`
    ),
  rollback: (id: number, key: string, version: number) =>
    req<Variable>(
      "POST",
      `/api/services/${id}/variables/${encodeURIComponent(key)}/rollback`,
      { version }
    ),

  audit: () => req<AuditEntry[]>("GET", "/api/audit"),

  // Export is a direct download (GET /api/export). Import posts a raw JSON doc.
  importConfig: async (jsonText: string) => {
    const res = await fetch("/api/import", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: jsonText,
      credentials: "same-origin",
    });
    if (!res.ok) {
      let msg = res.statusText;
      try {
        const j = await res.json();
        if (j.error) msg = j.error;
      } catch {
        /* ignore */
      }
      throw new ApiError(res.status, msg);
    }
    return res.json() as Promise<{
      services_created: number;
      services_existing: number;
      variables_imported: number;
    }>;
  },

  listUsers: () => req<User[]>("GET", "/api/users"),
  setUserRole: (id: number, role: "admin" | "user") =>
    req<void>("PUT", `/api/users/${id}/role`, { role }),
  listPermissions: (id: number) =>
    req<Permission[]>("GET", `/api/services/${id}/permissions`),
  setPermission: (id: number, email: string, level: "viewer" | "editor") =>
    req<void>("PUT", `/api/services/${id}/permissions`, { email, level }),
  revokePermission: (id: number, userId: number) =>
    req<void>("DELETE", `/api/services/${id}/permissions/${userId}`),
};
