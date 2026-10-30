import { useState } from "react";
import {
    Alert,
    Button,
    Dialog,
    DialogActions,
    DialogContent,
    DialogTitle,
    MenuItem,
    TextField,
    Typography,
} from "@mui/material";
import { useQueryClient } from "@tanstack/react-query";
import { esc, useResource, type Token } from "../api/hooks";
import { request } from "../api/client";
import {
    Confirm,
    Copy,
    PageTitle,
    QueryState,
    State,
} from "../components/common";
import { ResourceTable } from "../components/ResourceTable";
import { fieldError, OperationDialog } from "../components/OperationDialog";

export function Tokens() {
    const q = useResource<Token[]>("/api/v1/tokens");
    const [add, setAdd] = useState(false);
    const [name, setName] = useState("");
    const [days, setDays] = useState(90);
    const [created, setCreated] = useState<string>();
    const [remove, setRemove] = useState<Token>();
    const queries = useQueryClient();
    return (
        <>
            <PageTitle
                title="API tokens"
                description="Manage credentials used to access the Vmango API."
                count={q.data?.length}
                action={
                    <Button
                        variant="contained"
                        onClick={() => {
                            setName("");
                            setDays(90);
                            setAdd(true);
                        }}
                    >
                        Create token
                    </Button>
                }
            />
            <Typography color="text.secondary" sx={{ mb: 3 }}>
                Personal tokens grant your application-wide resource
                permissions. They can manage machines, volumes and keys. They
                cannot manage tokens or issue console sharing links. Browser
                logout does not revoke them.
            </Typography>
            <QueryState query={q}>
                {(rows) => (
                    <ResourceTable
                        rows={rows}
                        rowKey={(t) => t.id}
                        search={(t) => t.name}
                        columns={[
                            { id: "name", label: "Name", value: (t) => t.name },
                            {
                                id: "created",
                                label: "Created",
                                value: (t) => t.created_at,
                                render: (t) =>
                                    new Date(t.created_at).toLocaleDateString(),
                            },
                            {
                                id: "expiry",
                                label: "Expires",
                                value: (t) => t.expires_at,
                                render: (t) =>
                                    new Date(t.expires_at).toLocaleString(),
                            },
                            {
                                id: "used",
                                label: "Approximate last use",
                                value: (t) => t.last_used_at || "",
                                render: (t) =>
                                    t.last_used_at
                                        ? new Date(
                                              t.last_used_at,
                                          ).toLocaleString()
                                        : "Never",
                            },
                            {
                                id: "status",
                                label: "Status",
                                value: (t) => t.status,
                                render: (t) => <State value={t.status} />,
                            },
                        ]}
                        actions={(t) => (
                            <Button
                                color="error"
                                disabled={t.status !== "active"}
                                onClick={() => setRemove(t)}
                            >
                                Revoke
                            </Button>
                        )}
                        empty="No personal tokens. Create one for a CLI or CI job."
                    />
                )}
            </QueryState>
            {add && (
                <OperationDialog
                    title="Create API token"
                    action="Create token"
                    onClose={() => setAdd(false)}
                    run={async () => {
                        const result = await request<{ token: string }>(
                            "/api/v1/tokens",
                            "POST",
                            { name, days },
                        );
                        setCreated(result.token);
                    }}
                >
                    {(error) => (
                        <>
                            <TextField
                                label="Token name"
                                name="name"
                                required
                                value={name}
                                onChange={(e) => setName(e.target.value)}
                                placeholder="CI provisioning"
                                {...fieldError(error, "name")}
                            />
                            <TextField
                                select
                                label="Expires in"
                                value={days}
                                onChange={(e) =>
                                    setDays(Number(e.target.value))
                                }
                            >
                                {[30, 90, 365].map((n) => (
                                    <MenuItem value={n} key={n}>
                                        {n} days
                                    </MenuItem>
                                ))}
                            </TextField>
                            <Alert severity="info">
                                Store the token in a protected client file,
                                keychain or CI secret. It will be shown only
                                once.
                            </Alert>
                        </>
                    )}
                </OperationDialog>
            )}
            {created && (
                <Dialog open aria-labelledby="token-title">
                    <DialogTitle id="token-title">Save your token</DialogTitle>
                    <DialogContent>
                        <Alert severity="warning" sx={{ mb: 2 }}>
                            This token is shown once. Treat it like a password.
                        </Alert>
                        <TextField
                            label="API token"
                            multiline
                            minRows={5}
                            value={created}
                            slotProps={{ input: { readOnly: true } }}
                        />
                        <Copy text={created} label="Copy API token" />
                        <Typography variant="body2">
                            Send it as Authorization: Bearer TOKEN over HTTPS.
                            Replace it before expiry.
                        </Typography>
                    </DialogContent>
                    <DialogActions>
                        <Button onClick={() => setCreated(undefined)}>
                            I have saved the token
                        </Button>
                    </DialogActions>
                </Dialog>
            )}
            {remove && (
                <Confirm
                    title={`Revoke ${remove.name}?`}
                    action="Revoke token"
                    onClose={() => setRemove(undefined)}
                    run={async () => {
                        await request(
                            `/api/v1/tokens/${esc(remove.id)}`,
                            "DELETE",
                        );
                        await queries.invalidateQueries();
                    }}
                >
                    This token will fail subsequent requests and its active
                    native console connections will close. Already accepted
                    machine operations are not undone.
                </Confirm>
            )}
        </>
    );
}
