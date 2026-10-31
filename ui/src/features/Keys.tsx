import { useState } from "react";
import {
    Box,
    Breadcrumbs,
    Button,
    Link,
    TextField,
    Typography,
} from "@mui/material";
import { Link as RouterLink, useParams } from "react-router";
import { useQueryClient } from "@tanstack/react-query";
import { esc, useResource, type Key } from "../api/hooks";
import { request } from "../api/client";
import { Confirm, Copy, PageTitle, QueryState } from "../components/common";
import { ResourceTable } from "../components/ResourceTable";
import { fieldError, OperationDialog } from "../components/OperationDialog";

export function Keys() {
    const q = useResource<Key[]>("/api/v1/keys");
    const [add, setAdd] = useState(false);
    const [remove, setRemove] = useState<Key>();
    const [key, setKey] = useState("");
    const queries = useQueryClient();
    return (
        <>
            <PageTitle
                title="Keys"
                description="Manage SSH public keys available for machine provisioning."
                count={q.data?.length}
                action={
                    <Button
                        variant="contained"
                        onClick={() => {
                            setKey("");
                            setAdd(true);
                        }}
                    >
                        Add key
                    </Button>
                }
            />
            <QueryState query={q}>
                {(rows) => (
                    <ResourceTable
                        rows={rows}
                        rowKey={(k) => k.fingerprint}
                        search={(k) =>
                            `${k.comment} ${k.fingerprint} ${k.type}`
                        }
                        columns={[
                            {
                                id: "comment",
                                label: "Name / comment",
                                value: (k) => k.comment || "—",
                            },
                            { id: "type", label: "Type", value: (k) => k.type },
                            {
                                id: "fingerprint",
                                label: "Fingerprint",
                                value: (k) => k.fingerprint,
                                render: (k) => (
                                    <>
                                        <Link
                                            component={RouterLink}
                                            to={`/keys/${esc(k.fingerprint)}/show/`}
                                            className="mono"
                                        >
                                            {k.fingerprint}
                                        </Link>
                                        <Copy
                                            label={`Copy fingerprint ${k.fingerprint}`}
                                            text={k.fingerprint}
                                        />
                                    </>
                                ),
                            },
                        ]}
                        actions={(k) => (
                            <>
                                <Copy
                                    label={`Copy public key ${k.comment || k.fingerprint}`}
                                    text={k.public_key}
                                />
                                <Button
                                    color="error"
                                    onClick={() => setRemove(k)}
                                >
                                    Delete
                                </Button>
                            </>
                        )}
                        empty="No SSH public keys have been added."
                    />
                )}
            </QueryState>
            {add && (
                <OperationDialog
                    title="Add SSH public key"
                    action="Add key"
                    onClose={() => setAdd(false)}
                    run={() =>
                        request("/api/v1/keys", "POST", { public_key: key })
                    }
                >
                    {(error) => (
                        <TextField
                            label="SSH public key"
                            name="public_key"
                            required
                            multiline
                            minRows={4}
                            value={key}
                            onChange={(e) => setKey(e.target.value)}
                            {...fieldError(error, "public_key")}
                        />
                    )}
                </OperationDialog>
            )}
            {remove && (
                <Confirm
                    title="Delete SSH key?"
                    action="Delete key"
                    onClose={() => setRemove(undefined)}
                    run={async () => {
                        await request(
                            `/api/v1/keys/${esc(remove.fingerprint)}`,
                            "DELETE",
                        );
                        await queries.invalidateQueries({
                            predicate: (q) =>
                                q.queryKey[0] !== "administration",
                        });
                    }}
                >
                    Remove {remove.comment || remove.fingerprint} from the key
                    catalog. Existing guests retain keys already provisioned.
                </Confirm>
            )}
        </>
    );
}

export function KeyDetail() {
    const { fingerprint = "" } = useParams();
    const query = useResource<Key>(
        `/api/v1/keys/${esc(fingerprint)}`,
        true,
        false,
    );
    return (
        <>
            <Breadcrumbs sx={{ mb: 2 }}>
                <Link component={RouterLink} to="/keys/">
                    Keys
                </Link>
                <Typography>Public key</Typography>
            </Breadcrumbs>
            <PageTitle title="SSH public key" />
            <QueryState query={query}>
                {(key) => (
                    <Box sx={{ maxWidth: 900 }}>
                        <Typography sx={{ mb: 2 }}>
                            {key.comment || key.type}
                        </Typography>
                        <TextField
                            label="Fingerprint"
                            value={key.fingerprint}
                            slotProps={{ input: { readOnly: true } }}
                        />
                        <TextField
                            label="Public key"
                            multiline
                            minRows={4}
                            value={key.public_key}
                            slotProps={{ input: { readOnly: true } }}
                            sx={{ mt: 3 }}
                        />
                        <Copy
                            label="Copy SSH public key"
                            text={key.public_key}
                        />
                    </Box>
                )}
            </QueryState>
        </>
    );
}
