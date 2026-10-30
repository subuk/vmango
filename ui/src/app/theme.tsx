import {
    createContext,
    useContext,
    useMemo,
    useState,
    useEffect,
    type ReactNode,
} from "react";
import { createTheme, ThemeProvider, CssBaseline } from "@mui/material";

type Preference = "light" | "dark" | "system";
const Context = createContext({
    preference: "system" as Preference,
    setPreference: (_: Preference) => {},
    mode: "light" as "light" | "dark",
});
export const useAppearance = () => useContext(Context);
export function Appearance({ children }: { children: ReactNode }) {
    const [preference, set] = useState<Preference>(() => {
        try {
            const p = localStorage.getItem("vmango.theme");
            return p === "light" || p === "dark" ? p : "system";
        } catch {
            return "system";
        }
    });
    const [systemDark, setSystemDark] = useState(
        () => matchMedia("(prefers-color-scheme: dark)").matches,
    );
    useEffect(() => {
        const m = matchMedia("(prefers-color-scheme: dark)");
        const change = () => setSystemDark(m.matches);
        m.addEventListener("change", change);
        return () => m.removeEventListener("change", change);
    }, []);
    const mode =
        preference === "system" ? (systemDark ? "dark" : "light") : preference;
    useEffect(() => {
        document.documentElement.dataset.theme = mode;
        document.documentElement.style.colorScheme = mode;
        document.documentElement.style.backgroundColor =
            mode === "dark" ? "#171b20" : "#fafbfc";
    }, [mode]);
    const setPreference = (p: Preference) => {
        set(p);
        try {
            localStorage.setItem("vmango.theme", p);
        } catch {
            /* OS defaults remain usable without storage. */
        }
    };
    const theme = useMemo(
        () =>
            createTheme({
                palette: {
                    mode,
                    primary: { main: mode === "light" ? "#245a91" : "#8cbcf0" },
                    background: {
                        default: mode === "light" ? "#fafbfc" : "#171b20",
                        paper: mode === "light" ? "#ffffff" : "#20262d",
                    },
                    text: {
                        primary: mode === "light" ? "#252b32" : "#edf0f4",
                        secondary: mode === "light" ? "#535f6b" : "#b3bdc9",
                    },
                    divider: mode === "light" ? "#d4dbe2" : "#45505c",
                    action: {
                        disabled: mode === "light" ? "#63707d" : "#9ba7b5",
                        disabledBackground:
                            mode === "light" ? "#e5eaf0" : "#313b46",
                    },
                    success: { main: mode === "light" ? "#287144" : "#8bd6a0" },
                    warning: { main: mode === "light" ? "#875200" : "#efc477" },
                    error: { main: mode === "light" ? "#b42e35" : "#ff9b9e" },
                },
                shape: { borderRadius: 5 },
                typography: {
                    fontFamily:
                        '-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif',
                    fontSize: 14,
                    h1: { fontSize: "1.75rem", fontWeight: 600 },
                    h2: { fontSize: "1.2rem", fontWeight: 600 },
                    button: { textTransform: "none", fontWeight: 600 },
                },
                components: {
                    MuiButton: { defaultProps: { disableElevation: true } },
                    MuiTableCell: {
                        styleOverrides: {
                            root: { padding: "6px 14px", fontSize: 14 },
                            head: { fontWeight: 600, padding: "10px 14px" },
                        },
                    },
                    MuiOutlinedInput: {
                        styleOverrides: {
                            notchedOutline: {
                                borderColor:
                                    mode === "light" ? "#7c8997" : "#7c8c9c",
                            },
                        },
                    },
                    MuiPaper: {
                        defaultProps: { elevation: 0 },
                        styleOverrides: { root: { backgroundImage: "none" } },
                    },
                    MuiTextField: {
                        defaultProps: { size: "small", fullWidth: true },
                    },
                    MuiDialog: {
                        defaultProps: { fullWidth: true, maxWidth: "sm" },
                    },
                    MuiButtonBase: {
                        styleOverrides: {
                            root: {
                                "&.Mui-focusVisible": {
                                    outline: "2px solid",
                                    outlineOffset: 3,
                                },
                            },
                        },
                    },
                },
            }),
        [mode],
    );
    return (
        <Context.Provider value={{ preference, setPreference, mode }}>
            <ThemeProvider theme={theme}>
                <CssBaseline />
                {children}
            </ThemeProvider>
        </Context.Provider>
    );
}
