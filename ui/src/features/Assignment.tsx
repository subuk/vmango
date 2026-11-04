import { useState } from "react";
import { Alert, Button, Link, Stack, Typography } from "@mui/material";
import { useParams } from "react-router";
import {
    useRequest,
    useWorkspace,
    WorkspaceLink as RouterLink,
} from "../app/Workspace";
import { type TaskReference, APIError } from "../api/client";
import type { components } from "../api/schema";
import { ErrorMessage, PageTitle } from "../components/common";
import { ResourceTable } from "../components/ResourceTable";

type Preview = components["schemas"]["ApiAssignmentPreview"];
export function Assignment({ kind }: { kind: "machines" | "volumes" }) {
    const { cloud = "", id = "" } = useParams();
    const { namespace } = useWorkspace();
    const request = useRequest();
    const [preview, setPreview] = useState<Preview>();
    const [accepted, setAccepted] = useState<TaskReference>();
    const [error, setError] = useState<unknown>();
    const [busy, setBusy] = useState(false);
    const base = `/api/v1/clouds/${encodeURIComponent(cloud)}/${kind}/${encodeURIComponent(id)}/assign`;
    const execute = async (apply: boolean) => {
        if (busy) return;
        setBusy(true);
        setError(undefined);
        try {
            if (apply)
                setAccepted(await request<TaskReference>(base, "POST", {}));
            else
                setPreview(
                    await request<Preview>(`${base}/preview`, "POST", {}),
                );
        } catch (failure) {
            setError(failure);
        } finally {
            setBusy(false);
        }
    };
    const recover = error instanceof APIError ? error.recover : undefined;
    return (
        <Stack spacing={3}>
            <PageTitle
                title="Assign resource graph"
                description={`Destination namespace: ${namespace}. Cloud: ${cloud}.`}
            />
            <Typography>
                Preview the machine and disk graph before assigning unassigned
                resources. Existing assignments to another namespace prevent
                this operation. Assignment does not grant deletion ownership.
            </Typography>
            <Typography sx={{ overflowWrap: "anywhere" }}>
                Resource: {id}
            </Typography>
            <ErrorMessage error={error} />
            {accepted ? (
                <Alert severity="success">
                    Assignment accepted.{" "}
                    <Link
                        component={RouterLink}
                        to={`/tasks/${encodeURIComponent(accepted.task.id)}/`}
                    >
                        Track task
                    </Link>
                </Alert>
            ) : (
                <>
                    <Button disabled={busy} onClick={() => void execute(false)}>
                        Preview assignment
                    </Button>
                    {preview && (
                        <>
                            <Typography variant="h2">Machines</Typography>
                            <ResourceTable
                                rows={preview.machines || []}
                                rowKey={(v) => v.id}
                                search={(v) => `${v.name} ${v.id}`}
                                columns={[
                                    {
                                        id: "name",
                                        label: "Name",
                                        value: (v) => v.name,
                                    },
                                    {
                                        id: "namespace",
                                        label: "Current namespace",
                                        value: (v) =>
                                            v.namespace_id || "Unassigned",
                                    },
                                    {
                                        id: "id",
                                        label: "ID",
                                        value: (v) => v.id,
                                    },
                                ]}
                            />
                            <Typography variant="h2">Volumes</Typography>
                            <ResourceTable
                                rows={preview.volumes || []}
                                rowKey={(v) => v.id}
                                search={(v) => `${v.name} ${v.id}`}
                                columns={[
                                    {
                                        id: "name",
                                        label: "Name",
                                        value: (v) => v.name,
                                    },
                                    {
                                        id: "namespace",
                                        label: "Current namespace",
                                        value: (v) =>
                                            v.namespace_id || "Unassigned",
                                    },
                                    {
                                        id: "id",
                                        label: "ID",
                                        value: (v) => v.id,
                                    },
                                ]}
                            />
                            <Button
                                variant="contained"
                                disabled={busy || (!!error && !recover)}
                                onClick={async () => {
                                    if (!recover) return void execute(true);
                                    setBusy(true);
                                    try {
                                        setAccepted(
                                            (await recover()) as TaskReference,
                                        );
                                        setError(undefined);
                                    } catch (failure) {
                                        setError(failure);
                                    } finally {
                                        setBusy(false);
                                    }
                                }}
                            >
                                {recover
                                    ? "Recover assignment submission"
                                    : busy
                                      ? "Submitting…"
                                      : `Assign to ${namespace}`}
                            </Button>
                        </>
                    )}
                </>
            )}
        </Stack>
    );
}
