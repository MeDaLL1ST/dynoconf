import { useQuery } from "@tanstack/react-query";
import { api, ApiError, type AuditEntry } from "@/lib/api";
import { formatTime } from "@/lib/utils";
import {
  Badge,
  Card,
  CardContent,
  EmptyState,
  ErrorState,
  LoadingState,
} from "@/components/ui";

export function Audit() {
  const audit = useQuery<AuditEntry[], ApiError>({
    queryKey: ["audit"],
    queryFn: api.audit,
  });

  return (
    <div>
      <div className="mb-6">
        <h1 className="text-2xl font-semibold">Audit log</h1>
        <p className="text-sm text-muted-foreground">
          Who changed what, and when. Covers services, variables, permissions and
          logins.
        </p>
      </div>

      {audit.isLoading && <LoadingState />}
      {audit.error && <ErrorState message={audit.error.message} />}
      {audit.data?.length === 0 && (
        <EmptyState title="Nothing logged yet" hint="Actions will appear here." />
      )}

      {audit.data && audit.data.length > 0 && (
        <Card>
          <CardContent className="p-0">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border text-left text-xs uppercase tracking-wide text-muted-foreground">
                  <th className="px-5 py-3 font-medium">When</th>
                  <th className="px-5 py-3 font-medium">Actor</th>
                  <th className="px-5 py-3 font-medium">Action</th>
                  <th className="px-5 py-3 font-medium">Target</th>
                  <th className="px-5 py-3 font-medium">Details</th>
                </tr>
              </thead>
              <tbody>
                {audit.data.map((e) => (
                  <tr key={e.id} className="border-b border-border/60 last:border-0">
                    <td className="whitespace-nowrap px-5 py-2.5 text-muted-foreground">
                      {formatTime(e.created_at)}
                    </td>
                    <td className="px-5 py-2.5">{e.actor}</td>
                    <td className="px-5 py-2.5">
                      <Badge variant="secondary">{e.action}</Badge>
                    </td>
                    <td className="px-5 py-2.5 font-mono text-xs">{e.target}</td>
                    <td className="px-5 py-2.5 font-mono text-xs text-muted-foreground">
                      {Object.keys(e.details || {}).length > 0
                        ? JSON.stringify(e.details)
                        : "—"}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
