import { Alert, Box, Button, Link, Stack, Typography } from "@mui/material";
import { Link as RouterLink, useParams, useSearchParams } from "react-router";
import { type Namespace } from "../app/Workspace";
import { useMe } from "../app/Auth";
import { adminCloudURL, useAdministration } from "../api/administration";
import { PageTitle, QueryState, Copy } from "../components/common";
import { DetailSection } from "../components/Information";
import { ResourceTable } from "../components/ResourceTable";

export function NamespaceConfiguration() {
    const { namespace: id } = useParams();
    const principal = useMe()?.id;
    const [params, setParams] = useSearchParams();
    const selected = id || params.get("namespace_selected");
    const q = useAdministration<{ items: Namespace[] }>(
        "/api/v1/administration/configuration/namespaces",
    );
    const listURL = `/administration/configuration/namespaces/${params.toString() ? `?${params}` : ""}`;
    return (
        <>
            <PageTitle
                title={id ? "Namespace configuration" : "Namespaces"}
                description="Accepted workspace definitions and cloud admission. Changes activate on restart."
                action={
                    <Stack direction="row" sx={{ gap: 1 }}>
                        {id && (
                            <Button component={RouterLink} to={listURL}>
                                All namespaces
                            </Button>
                        )}
                        <Button onClick={() => void q.refetch()}>
                            Refresh
                        </Button>
                    </Stack>
                }
            />
            <QueryState query={q}>
                {(data) => {
                    const namespace = data.items.find((n) => n.id === selected);
                    return (
                        <Stack spacing={2}>
                            {!id && (
                                <ResourceTable
                                    rows={data.items}
                                    rowKey={(n) => n.id}
                                    search={(n) =>
                                        `${n.id} ${n.name} ${n.description} ${(n.clouds || []).join(" ")}`
                                    }
                                    preferenceKey={`administration:${principal}:namespaces`}
                                    selectedKey={selected || undefined}
                                    onSelect={(n) =>
                                        setParams((old) => {
                                            const next = new URLSearchParams(
                                                old,
                                            );
                                            next.set(
                                                "namespace_selected",
                                                n.id,
                                            );
                                            return next;
                                        })
                                    }
                                    columns={[
                                        {
                                            id: "name",
                                            label: "Name",
                                            value: (n) => n.name,
                                        },
                                        {
                                            id: "id",
                                            label: "ID",
                                            value: (n) => n.id,
                                        },
                                        {
                                            id: "description",
                                            label: "Description",
                                            value: (n) => n.description || "—",
                                        },
                                        {
                                            id: "clouds",
                                            label: "Allowed clouds",
                                            value: (n) =>
                                                (n.clouds || []).join(", "),
                                            render: (n) => (
                                                <Stack
                                                    direction="row"
                                                    sx={{
                                                        gap: 1,
                                                        flexWrap: "wrap",
                                                    }}
                                                >
                                                    {(n.clouds || []).map(
                                                        (c) => (
                                                            <Link
                                                                key={c}
                                                                component={
                                                                    RouterLink
                                                                }
                                                                to={adminCloudURL(
                                                                    c,
                                                                )}
                                                            >
                                                                {c}
                                                            </Link>
                                                        ),
                                                    )}
                                                </Stack>
                                            ),
                                        },
                                    ]}
                                    empty="No namespaces are configured."
                                />
                            )}
                            {selected && !namespace && (
                                <Alert severity="error">
                                    Namespace not found in accepted
                                    configuration.
                                </Alert>
                            )}
                            {namespace && (
                                <DetailSection
                                    title={namespace.name}
                                    description={
                                        namespace.description ||
                                        "Namespace configuration"
                                    }
                                >
                                    <Stack
                                        direction="row"
                                        sx={{
                                            justifyContent: "space-between",
                                            gap: 1,
                                            flexWrap: "wrap",
                                            mb: 1,
                                        }}
                                    >
                                        <Typography variant="body2">
                                            ID: {namespace.id}{" "}
                                            <Copy
                                                text={namespace.id}
                                                label="Copy namespace ID"
                                            />
                                        </Typography>
                                        {!id && (
                                            <Stack direction="row">
                                                <Button
                                                    component={RouterLink}
                                                    to={`/administration/configuration/namespaces/${encodeURIComponent(namespace.id)}/?${params}`}
                                                >
                                                    Full details
                                                </Button>
                                                <Button
                                                    onClick={() =>
                                                        setParams((old) => {
                                                            const next =
                                                                new URLSearchParams(
                                                                    old,
                                                                );
                                                            next.delete(
                                                                "namespace_selected",
                                                            );
                                                            return next;
                                                        })
                                                    }
                                                >
                                                    Close details
                                                </Button>
                                            </Stack>
                                        )}
                                    </Stack>
                                    <Typography
                                        variant="body2"
                                        color="text.secondary"
                                        sx={{ mb: 1 }}
                                    >
                                        Inventory links filter Administration
                                        only. They do not select a workload
                                        namespace.
                                    </Typography>
                                    {(namespace.clouds || []).length ? (
                                        <Box
                                            component="ul"
                                            sx={{ pl: 2, m: 0 }}
                                        >
                                            {namespace.clouds.map((cloud) => (
                                                <Box
                                                    component="li"
                                                    key={cloud}
                                                    sx={{ py: 1 }}
                                                >
                                                    <Link
                                                        component={RouterLink}
                                                        to={adminCloudURL(
                                                            cloud,
                                                        )}
                                                    >
                                                        {cloud}
                                                    </Link>
                                                    <Stack
                                                        direction="row"
                                                        sx={{
                                                            gap: 2,
                                                            flexWrap: "wrap",
                                                            mt: 0.5,
                                                        }}
                                                    >
                                                        <Link
                                                            component={
                                                                RouterLink
                                                            }
                                                            to={`${adminCloudURL(cloud)}machines/?inventory_namespace=${encodeURIComponent(namespace.id)}`}
                                                        >
                                                            Machine inventory
                                                        </Link>
                                                        <Link
                                                            component={
                                                                RouterLink
                                                            }
                                                            to={`${adminCloudURL(cloud)}volumes/?inventory_namespace=${encodeURIComponent(namespace.id)}`}
                                                        >
                                                            Volume inventory
                                                        </Link>
                                                        <Link
                                                            component={
                                                                RouterLink
                                                            }
                                                            to={`${adminCloudURL(cloud)}configuration/`}
                                                        >
                                                            Cloud configuration
                                                        </Link>
                                                    </Stack>
                                                </Box>
                                            ))}
                                        </Box>
                                    ) : (
                                        <Typography color="text.secondary">
                                            No clouds are admitted.
                                        </Typography>
                                    )}
                                    <Button
                                        component={RouterLink}
                                        to={`/administration/tasks/?namespace_id=${encodeURIComponent(namespace.id)}`}
                                        sx={{ mt: 1 }}
                                    >
                                        Namespace task history
                                    </Button>
                                </DetailSection>
                            )}
                        </Stack>
                    );
                }}
            </QueryState>
        </>
    );
}
export { AdministrationInventory } from "./AdministrationResources";
export { AdministrationTasks } from "./AdministrationOperations";
