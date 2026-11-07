import { Alert, Breadcrumbs } from "@mui/material";
import { Route, Routes, useParams } from "react-router";
import {
    adminCloudPath,
    useAdministration,
    type AdminCloud,
    type Installation,
} from "../api/administration";
import { QueryState } from "../components/common";
import {
    AdministrationInventory,
    NamespaceConfiguration,
} from "./AdministrationInventory";
import { AdministrationTasks } from "./AdministrationOperations";
import { TaskDetail } from "./Tasks";
import {
    AdministrationHosts,
    AdministrationHostDetail,
} from "./AdministrationHosts";
import {
    InstallationPage,
    CloudConfiguration,
    Clouds,
} from "./AdministrationConfiguration";
import {
    AdministrationOverview,
    CloudOverview,
} from "./AdministrationOverview";
import { Images, ImageDetail } from "./AdministrationImages";
import { link } from "./AdministrationFields";
import { adminCloudURL } from "../api/administration";
const root = "/api/v1/administration";
export function Administration() {
    const snapshot = useAdministration<Installation>(`${root}/configuration`);
    return (
        <>
            <Routes>
                <Route index element={<AdministrationOverview />} />
                <Route
                    path="configuration/namespaces/"
                    element={<NamespaceConfiguration />}
                />
                <Route
                    path="configuration/namespaces/:namespace/"
                    element={<NamespaceConfiguration />}
                />
                <Route path="tasks/" element={<AdministrationTasks />} />
                <Route
                    path="tasks/:id/"
                    element={<TaskDetail administration />}
                />
                <Route path="configuration/" element={<InstallationPage />} />
                <Route
                    path="clouds/"
                    element={<Clouds snapshot={snapshot.data?.snapshot_id} />}
                />
                <Route
                    path="clouds/:cloud/*"
                    element={
                        <CloudPages snapshot={snapshot.data?.snapshot_id} />
                    }
                />
                <Route
                    path="*"
                    element={<Alert severity="error">Page not found.</Alert>}
                />
            </Routes>
        </>
    );
}
function CloudPages({ snapshot }: { snapshot?: string }) {
    const { cloud = "" } = useParams();
    const q = useAdministration<AdminCloud>(
        adminCloudPath(cloud),
        true,
        snapshot,
    );
    return (
        <QueryState query={q}>
            {(c) => (
                <>
                    <Breadcrumbs sx={{ mb: 2 }}>
                        {link("/administration/clouds/", "Clouds")}
                        {link(adminCloudURL(c.id), c.name)}
                    </Breadcrumbs>
                    <Routes>
                        <Route
                            path="machines/"
                            element={
                                <AdministrationInventory kind="machines" />
                            }
                        />
                        <Route
                            path="machines/:id/"
                            element={
                                <AdministrationInventory kind="machines" />
                            }
                        />
                        <Route
                            path="volumes/"
                            element={<AdministrationInventory kind="volumes" />}
                        />
                        <Route
                            path="volumes/:id/"
                            element={<AdministrationInventory kind="volumes" />}
                        />
                        <Route
                            index
                            element={
                                <CloudOverview
                                    cloud={c}
                                    refresh={() => void q.refetch()}
                                />
                            }
                        />
                        <Route
                            path="configuration/"
                            element={
                                <CloudConfiguration
                                    cloud={c}
                                    snapshot={snapshot}
                                />
                            }
                        />
                        <Route
                            path="configuration/hosts/"
                            element={
                                c.capabilities.host_inventory ? (
                                    <AdministrationHosts
                                        cloud={c}
                                        snapshot={snapshot}
                                    />
                                ) : (
                                    <NotFound />
                                )
                            }
                        />
                        <Route
                            path="configuration/hosts/:node/"
                            element={
                                c.capabilities.host_inventory ? (
                                    <AdministrationHostDetail
                                        initialTab="configuration"
                                        cloud={c}
                                        snapshot={snapshot}
                                    />
                                ) : (
                                    <NotFound />
                                )
                            }
                        />
                        <Route
                            path="hosts/"
                            element={
                                c.capabilities.host_inventory ? (
                                    <AdministrationHosts cloud={c} />
                                ) : (
                                    <NotFound />
                                )
                            }
                        />
                        <Route
                            path="hosts/:node/"
                            element={
                                c.capabilities.host_inventory ? (
                                    <AdministrationHostDetail
                                        cloud={c}
                                        snapshot={snapshot}
                                    />
                                ) : (
                                    <NotFound />
                                )
                            }
                        />
                        <Route
                            path="images/"
                            element={<Images cloud={c} snapshot={snapshot} />}
                        />
                        <Route
                            path="images/:image/"
                            element={
                                <ImageDetail cloud={c} snapshot={snapshot} />
                            }
                        />
                        <Route path="*" element={<NotFound />} />
                    </Routes>
                </>
            )}
        </QueryState>
    );
}
function NotFound() {
    return <Alert severity="error">Page not found.</Alert>;
}
