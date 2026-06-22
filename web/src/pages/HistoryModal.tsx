import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { RotateCcw } from "lucide-react";
import { api, ApiError, type VariableVersion } from "@/lib/api";
import { formatTime } from "@/lib/utils";
import { Badge, Button, ErrorState, LoadingState } from "@/components/ui";
import { Modal } from "@/components/Modal";

const changeVariant: Record<string, "success" | "secondary" | "warning" | "default"> = {
  create: "success",
  update: "secondary",
  delete: "warning",
  rollback: "default",
};

export function HistoryModal({
  serviceId,
  variableKey,
  canEdit,
  onClose,
}: {
  serviceId: number;
  variableKey: string;
  canEdit: boolean;
  onClose: () => void;
}) {
  const qc = useQueryClient();
  const history = useQuery<VariableVersion[], ApiError>({
    queryKey: ["history", serviceId, variableKey],
    queryFn: () => api.variableHistory(serviceId, variableKey),
  });

  const rollback = useMutation({
    mutationFn: (version: number) => api.rollback(serviceId, variableKey, version),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["variables", serviceId] });
      qc.invalidateQueries({ queryKey: ["history", serviceId, variableKey] });
    },
  });

  const rows = history.data ?? [];

  return (
    <Modal open onClose={onClose} title={`History — ${variableKey}`}>
      {history.isLoading && <LoadingState />}
      {history.error && <ErrorState message={history.error.message} />}
      {rollback.error && <ErrorState message={(rollback.error as ApiError).message} />}

      <ol className="max-h-[60vh] space-y-3 overflow-y-auto pr-1">
        {rows.map((h, i) => {
          const prev = rows[i + 1];
          const changed = prev && prev.value !== h.value;
          return (
            <li
              key={h.id}
              className="rounded-md border border-border p-3 text-sm"
            >
              <div className="mb-1.5 flex items-center justify-between gap-2">
                <div className="flex items-center gap-2">
                  <Badge variant="secondary">v{h.version}</Badge>
                  <Badge variant={changeVariant[h.change_type] ?? "secondary"}>
                    {h.change_type}
                  </Badge>
                </div>
                {canEdit && h.change_type !== "delete" && (
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => rollback.mutate(h.version)}
                    disabled={rollback.isPending}
                  >
                    <RotateCcw className="h-3.5 w-3.5" /> Roll back to this
                  </Button>
                )}
              </div>

              {h.change_type === "delete" ? (
                <p className="font-mono text-xs text-muted-foreground line-through">
                  {h.value || "(empty)"}
                </p>
              ) : (
                <div className="font-mono text-xs">
                  {changed && (
                    <span className="mr-2 text-muted-foreground line-through">
                      {prev!.value || "(empty)"}
                    </span>
                  )}
                  <span className={changed ? "text-emerald-500" : ""}>
                    {h.value || "(empty)"}
                  </span>
                </div>
              )}

              <p className="mt-1.5 text-xs text-muted-foreground">
                {h.changed_by} · {formatTime(h.changed_at)}
              </p>
            </li>
          );
        })}
      </ol>
    </Modal>
  );
}
