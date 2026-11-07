import { createRoot } from "react-dom/client";
import { Appearance } from "../app/theme";
import { Installer } from "./Installer";
import "../style.css";

createRoot(document.getElementById("root")!).render(
    <Appearance>
        <Installer />
    </Appearance>,
);
