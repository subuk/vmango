import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
    Alert,
    Box,
    Button,
    Dialog,
    DialogActions,
    DialogContent,
    DialogTitle,
    MenuItem,
    Stack,
    TextField,
    Typography,
} from "@mui/material";
import { APIError, request } from "../api/client";
import type { components } from "../api/schema";
import { esc, useResource } from "../api/hooks";
import {
    Confirm,
    ErrorMessage,
    PageTitle,
    QueryState,
} from "../components/common";
import { fieldError, OperationDialog } from "../components/OperationDialog";
import { ResourceTable } from "../components/ResourceTable";

type CloudInitTemplateSummary =
    components["schemas"]["APICloudInitTemplateSummary"];
type CloudInitTemplate = components["schemas"]["ApiCloudInitTemplate"];
const templatesPath = "/api/v1/cloud-init-templates";

function useTemplate(id: string) {
    return useQuery({
        queryKey: ["cloud-init-template", id],
        queryFn: ({ signal }) =>
            request<CloudInitTemplate>(
                `${templatesPath}/${esc(id)}`,
                "GET",
                undefined,
                signal,
            ),
        enabled: !!id,
        gcTime: 0,
        staleTime: Infinity,
        retry: false,
        refetchOnWindowFocus: false,
        refetchOnReconnect: false,
    });
}

function TemplateEditor({
    template,
    onClose,
    reload,
}: {
    template?: CloudInitTemplate;
    onClose: () => void;
    reload: () => Promise<unknown>;
}) {
    const saved = useRef(false);
    const [name, setName] = useState(template?.name || "");
    const [description, setDescription] = useState(template?.description || "");
    const [userData, setUserData] = useState(template?.user_data || "");
    const [discard, setDiscard] = useState<"close" | "reload">();
    const dirty =
        name !== (template?.name || "") ||
        description !== (template?.description || "") ||
        userData !== (template?.user_data || "");
    return (
        <>
            <OperationDialog
                title={
                    template
                        ? "Edit cloud-init template"
                        : "Add cloud-init template"
                }
                action={template ? "Save template" : "Add template"}
                onClose={() =>
                    !saved.current && dirty ? setDiscard("close") : onClose()
                }
                run={async () => {
                    await request(
                        template
                            ? `${templatesPath}/${esc(template.id)}`
                            : templatesPath,
                        template ? "PUT" : "POST",
                        {
                            name,
                            description,
                            user_data: userData,
                            ...(template
                                ? { revision: template.revision }
                                : {}),
                        },
                    );
                    saved.current = true;
                }}
            >
                {(error) => (
                    <>
                        <TextField
                            label="Name"
                            name="name"
                            required
                            value={name}
                            onChange={(e) => setName(e.target.value)}
                            {...fieldError(error, "name")}
                        />
                        <TextField
                            label="Description"
                            name="description"
                            value={description}
                            onChange={(e) => setDescription(e.target.value)}
                            {...fieldError(error, "description")}
                        />
                        <TextField
                            label="Cloud-init user-data"
                            name="user_data"
                            required
                            multiline
                            minRows={10}
                            value={userData}
                            onChange={(e) => setUserData(e.target.value)}
                            {...fieldError(error, "user_data")}
                            sx={{
                                minWidth: { sm: 480 },
                                "& textarea": { fontFamily: "monospace" },
                            }}
                        />
                        <Typography variant="body2" color="text.secondary">
                            Stored exactly as entered. Templates are shared with
                            other Vmango users. Changes apply only to future
                            machine requests.
                        </Typography>
                        {template &&
                            error instanceof APIError &&
                            error.status === 409 && (
                                <Alert
                                    severity="warning"
                                    action={
                                        <Button
                                            onClick={() => setDiscard("reload")}
                                        >
                                            Reload latest
                                        </Button>
                                    }
                                >
                                    This template changed. Reload it before
                                    saving again.
                                </Alert>
                            )}
                    </>
                )}
            </OperationDialog>
            {discard && (
                <Confirm
                    title="Discard unsaved changes?"
                    action="Discard changes"
                    onClose={() => setDiscard(undefined)}
                    run={async () => {
                        if (discard === "reload") await reload();
                        else onClose();
                    }}
                >
                    Your edits have not been saved.
                </Confirm>
            )}
        </>
    );
}

function ExistingTemplate({
    id,
    onClose,
}: {
    id: string;
    onClose: () => void;
}) {
    const query = useTemplate(id);
    if (query.data)
        return (
            <TemplateEditor
                key={query.data.revision}
                template={query.data}
                onClose={onClose}
                reload={() => query.refetch({ throwOnError: true })}
            />
        );
    return (
        <Dialog open onClose={onClose}>
            <DialogTitle>Cloud-init template</DialogTitle>
            <DialogContent>
                <QueryState query={query}>{() => null}</QueryState>
            </DialogContent>
            <DialogActions>
                <Button onClick={onClose}>Close</Button>
            </DialogActions>
        </Dialog>
    );
}

export function CloudInitTemplates() {
    const query = useResource<CloudInitTemplateSummary[]>(templatesPath);
    const queries = useQueryClient();
    const [edit, setEdit] = useState<string>();
    const [remove, setRemove] = useState<CloudInitTemplateSummary>();
    return (
        <>
            <PageTitle
                title="Cloud-init templates"
                description="Save reusable user-data for new machines."
                count={query.data?.length}
                action={
                    <Button variant="contained" onClick={() => setEdit("")}>
                        Add template
                    </Button>
                }
            />
            <QueryState query={query}>
                {(rows) => (
                    <ResourceTable
                        rows={rows}
                        rowKey={(t) => t.id}
                        search={(t) => `${t.name} ${t.description}`}
                        columns={[
                            {
                                id: "name",
                                label: "Name",
                                value: (t) => t.name,
                                render: (t) => (
                                    <Button onClick={() => setEdit(t.id)}>
                                        {t.name}
                                    </Button>
                                ),
                            },
                            {
                                id: "description",
                                label: "Description",
                                value: (t) => t.description || "—",
                            },
                        ]}
                        actions={(t) => (
                            <>
                                <Button onClick={() => setEdit(t.id)}>
                                    View / edit
                                </Button>
                                <Button
                                    color="error"
                                    onClick={() => setRemove(t)}
                                >
                                    Delete
                                </Button>
                            </>
                        )}
                        empty="No cloud-init templates have been added."
                    />
                )}
            </QueryState>
            {edit === "" && (
                <TemplateEditor
                    onClose={() => setEdit(undefined)}
                    reload={async () => {}}
                />
            )}
            {!!edit && (
                <ExistingTemplate
                    id={edit}
                    onClose={() => setEdit(undefined)}
                />
            )}
            {remove && (
                <OperationDialog
                    title="Delete cloud-init template?"
                    action="Delete template"
                    onClose={() => setRemove(undefined)}
                    run={async () => {
                        await request(
                            `${templatesPath}/${esc(remove.id)}`,
                            "DELETE",
                            { revision: remove.revision },
                        );
                    }}
                >
                    {(error) => (
                        <>
                            <Typography>
                                Remove {remove.name}? Existing machines and
                                accepted creation tasks retain their
                                configuration.
                            </Typography>
                            {error instanceof APIError &&
                                error.status === 409 && (
                                    <Alert
                                        severity="warning"
                                        action={
                                            <Button
                                                onClick={async () => {
                                                    await queries.invalidateQueries(
                                                        {
                                                            queryKey: [
                                                                templatesPath,
                                                            ],
                                                        },
                                                    );
                                                    setRemove(undefined);
                                                }}
                                            >
                                                Refresh list
                                            </Button>
                                        }
                                    >
                                        This template changed. Refresh the list
                                        and review it before deleting.
                                    </Alert>
                                )}
                        </>
                    )}
                </OperationDialog>
            )}
        </>
    );
}

export type CloudInitSelection = {
    user_data?: string;
    user_data_template?: components["schemas"]["CloudInitTemplateRef"];
    pending?: boolean;
};

export function CloudInitInput({
    disabled,
    onChange,
    conflict,
}: {
    disabled: boolean;
    onChange: (value: CloudInitSelection) => void;
    conflict?: boolean;
}) {
    const templates = useResource<CloudInitTemplateSummary[]>(templatesPath);
    const [source, setSource] = useState("none");
    const [userData, setUserData] = useState("");
    const [nextSource, setNextSource] = useState<string>();
    const template = useTemplate(
        source === "none" || source === "custom" || disabled ? "" : source,
    );
    const select = (value: string) => {
        if (source === "custom" && userData && value !== source)
            setNextSource(value);
        else setSource(value);
    };
    const activeTemplate = source !== "none" && source !== "custom";
    useEffect(() => {
        if (disabled || source === "none") onChange({});
        else if (source === "custom") onChange({ user_data: userData });
        else if (template.data)
            onChange({
                user_data_template: {
                    id: template.data.id,
                    revision: template.data.revision,
                },
            });
        else onChange({ pending: true });
    }, [disabled, source, userData, template.data, onChange]);
    return (
        <Box component="section" aria-label="Cloud-init configuration">
            <Stack spacing={2}>
                <TextField
                    select
                    label="Cloud-init user-data source"
                    value={source}
                    disabled={disabled}
                    onChange={(e) => select(e.target.value)}
                >
                    <MenuItem value="none">None</MenuItem>
                    <MenuItem value="custom">Custom</MenuItem>
                    {templates.data?.map((t) => (
                        <MenuItem key={t.id} value={t.id}>
                            {t.name}
                        </MenuItem>
                    ))}
                    {activeTemplate &&
                        !templates.data?.some((t) => t.id === source) && (
                            <MenuItem value={source}>
                                {template.data?.name || "Selected template"}
                            </MenuItem>
                        )}
                </TextField>
                <ErrorMessage error={templates.error} />
                {disabled ? (
                    <Alert severity="info">
                        This image has initialization disabled. Cloud-init
                        user-data will not be sent.
                    </Alert>
                ) : source === "custom" ? (
                    <TextField
                        label="Cloud-init user-data"
                        name="user_data"
                        multiline
                        minRows={6}
                        value={userData}
                        onChange={(e) => setUserData(e.target.value)}
                        sx={{ "& textarea": { fontFamily: "monospace" } }}
                    />
                ) : (
                    activeTemplate && (
                        <>
                            <QueryState query={template}>
                                {(t) => (
                                    <>
                                        {t.description && (
                                            <Typography color="text.secondary">
                                                {t.description}
                                            </Typography>
                                        )}
                                        <TextField
                                            label="Template preview"
                                            value={t.user_data}
                                            multiline
                                            minRows={6}
                                            slotProps={{
                                                input: { readOnly: true },
                                            }}
                                            sx={{
                                                "& textarea": {
                                                    fontFamily: "monospace",
                                                },
                                            }}
                                        />
                                        <Button
                                            sx={{ alignSelf: "flex-start" }}
                                            onClick={() => {
                                                setUserData(t.user_data);
                                                setSource("custom");
                                            }}
                                        >
                                            Customize for this machine
                                        </Button>
                                    </>
                                )}
                            </QueryState>
                            {(conflict || template.isError) && (
                                <Button onClick={() => template.refetch()}>
                                    Reload template
                                </Button>
                            )}
                        </>
                    )
                )}
            </Stack>
            {nextSource !== undefined && (
                <Confirm
                    title="Discard custom user-data?"
                    action="Discard changes"
                    onClose={() => setNextSource(undefined)}
                    run={async () => {
                        setUserData("");
                        setSource(nextSource);
                    }}
                >
                    Switching the source replaces your custom user-data.
                </Confirm>
            )}
        </Box>
    );
}
